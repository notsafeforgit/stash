package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestFileIngestMigrationPreservesExistingCaptureReceipt(t *testing.T) {
	fixture, repo := archiveTestDatabase(t)
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Retained producer", Kind: "feed", State: "active", Namespace: "native:reddit"}})
	var producer *models.IngestProducer
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		producer, err = repo.Ingest.CreateProducer(ctx, "Retained worker")
		return err
	}))
	service := ingest.New(repo)
	_, token, err := service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID}}, nil)
	require.NoError(t, err)
	event := ingest.CaptureEvent{Protocol: 1, ProducerUUID: producer.UUID, EventUUID: uuid.NewString(), RunUUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, Kind: "source.capture", ObservedAt: time.Now().UTC(), ExtractorVersion: "fixture", RetentionPolicy: archive.SourceRetentionVersion, Post: ingest.PostReference{Namespace: "native:reddit", Value: "kept-post"}, Source: []byte(`{"category":"reddit","id":"kept-post","author":"Example","author_fullname":"t2_example"}`)}
	body, err := json.Marshal(event)
	require.NoError(t, err)
	receipt, err := service.Capture(t.Context(), token, body, ingest.Digest(body))
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "schema20.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+20; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	_, err = raw.Exec("ATTACH DATABASE ? AS source_fixture", fixture.DatabasePath())
	require.NoError(t, err)
	tx, err := raw.Begin()
	require.NoError(t, err)
	for _, table := range []string{"source_accounts", "source_account_identifiers", "source_account_identifier_evidence", "source_collections", "source_collection_revisions", "source_posts", "source_post_identifiers", "source_payloads", "source_profile_bodies", "source_post_revisions", "source_captures", "source_capture_profiles", "source_collection_captures", "capture_publisher_decisions", "capture_publisher_claims", "ingest_producers", "ingest_credentials", "ingest_credential_scopes"} {
		copyHistoricalFixtureTable(t, tx, table)
	}
	_, err = tx.Exec(`INSERT INTO ingest_receipts SELECT producer_uuid,event_uuid,digest,credential_uuid,collection_uuid,collection_revision,root_uuid,run_uuid,kind,post_uuid,capture_uuid,result,committed_at FROM source_fixture.ingest_receipts`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	_, err = raw.Exec("DETACH DATABASE source_fixture")
	require.NoError(t, err)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	upgraded := ingest.New(db.Repository())
	retained, err := upgraded.Receipt(t.Context(), token, event.EventUUID)
	require.NoError(t, err)
	require.Equal(t, receipt, retained)
	require.Empty(t, retained.JobUUID)
	replay, err := upgraded.Capture(t.Context(), token, body, ingest.Digest(body))
	require.NoError(t, err)
	require.Equal(t, receipt, replay)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_jobs"))
}
