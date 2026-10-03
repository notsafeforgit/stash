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

func TestIngestRootMigrationPreservesPopulatedNamedAuthority(t *testing.T) {
	fixture, repo := archiveTestDatabase(t)
	root := putMediaRoot(t, repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Retained root", State: "active"}})
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Retained source", Kind: "feed", State: "active", Namespace: "native:reddit", RootUUID: &root.UUID, PathPrefix: "Retained"}})
	var producer *models.IngestProducer
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		producer, err = repo.Ingest.CreateProducer(ctx, "Existing worker")
		return err
	}))
	service := ingest.New(repo)
	credential, token, err := service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID, RootUUID: &root.UUID}}, nil)
	require.NoError(t, err)
	event := ingest.CaptureEvent{Protocol: 1, ProducerUUID: producer.UUID, EventUUID: uuid.NewString(), RunUUID: uuid.NewString(),
		CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, RootUUID: &root.UUID, Kind: "source.capture", ObservedAt: time.Now().UTC(),
		ExtractorVersion: "fixture", RetentionPolicy: archive.SourceRetentionVersion, Post: ingest.PostReference{Namespace: "native:reddit", Value: "kept-root-post"},
		Source: []byte(`{"category":"reddit","id":"kept-root-post","author":"Example","author_fullname":"t2_example"}`)}
	body, err := json.Marshal(event)
	require.NoError(t, err)
	captureReceipt, err := service.Capture(t.Context(), token, body, ingest.Digest(body))
	require.NoError(t, err)
	fileReceipt := *captureReceipt
	fileReceipt.EventUUID, fileReceipt.Kind = uuid.NewString(), "file.completed"
	fileReceipt.Digest = ingest.Digest([]byte("retained file admission"))
	fileReceipt.Result = json.RawMessage(`{"status":"queued","media_ingested":false}`)
	var file *models.IngestReceipt
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		job, err := repo.ArchiveJob.Submit(ctx, jobSubmission("migration-file", "migration-destination"), time.Now(), 100)
		require.NoError(t, err)
		fileReceipt.JobUUID = job.UUID
		file, err = repo.Ingest.RecordReceipt(ctx, fileReceipt)
		return err
	}))
	path := filepath.Join(t.TempDir(), "schema23.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+23; version = m.CurrentSchemaVersion() {
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
	for _, table := range []string{"media_roots", "media_root_revisions", "source_accounts", "source_account_identifiers", "source_account_identifier_evidence", "source_collections", "source_collection_revisions", "source_posts", "source_post_identifiers", "source_payloads", "source_profile_bodies", "source_post_revisions", "source_captures", "source_capture_profiles", "source_collection_captures", "capture_publisher_decisions", "capture_publisher_claims", "ingest_producers", "ingest_credentials", "ingest_credential_scopes", "archive_jobs", "archive_job_submissions", "ingest_receipts"} {
		copyHistoricalFixtureTable(t, tx, table)
	}
	require.NoError(t, tx.Commit())
	_, err = raw.Exec("DETACH DATABASE source_fixture")
	require.NoError(t, err)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	defer db.Close()
	upgraded := ingest.New(db.Repository())
	retainedCredential, err := upgraded.Authenticate(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, credential, retainedCredential)
	require.Empty(t, retainedCredential.RootUUIDs, "migration must not broaden existing authority")
	for _, receipt := range []*models.IngestReceipt{captureReceipt, file} {
		retained, err := upgraded.Receipt(t.Context(), token, receipt.EventUUID)
		require.NoError(t, err)
		require.Equal(t, receipt, retained)
	}
	replayed, err := upgraded.Capture(t.Context(), token, body, ingest.Digest(body))
	require.NoError(t, err)
	require.Equal(t, captureReceipt, replayed)
	_, err = raw.Exec("DELETE FROM ingest_credential_scopes")
	require.ErrorContains(t, err, "retained receipts")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM ingest_credential_roots"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM archive_jobs"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestIngestRootReceiptsRetainGrantEvidenceAndAnonymise(t *testing.T) {
	fixture, repo := archiveTestDatabase(t)
	root := putMediaRoot(t, repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Private root", State: "active"}})
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Root source", Kind: "feed", State: "active", Namespace: "native:reddit", RootUUID: &root.UUID, PathPrefix: "Root source"}})
	var producer *models.IngestProducer
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		producer, err = repo.Ingest.CreateProducer(ctx, "Root worker")
		return err
	}))
	service := ingest.New(repo)
	_, token, err := service.IssueCredential(t.Context(), producer.UUID, nil, nil, root.UUID)
	require.NoError(t, err)
	event := ingest.CaptureEvent{Protocol: 1, ProducerUUID: producer.UUID, EventUUID: uuid.NewString(), RunUUID: uuid.NewString(),
		CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, RootUUID: &root.UUID, Kind: "source.capture", ObservedAt: time.Now().UTC(),
		ExtractorVersion: "fixture", RetentionPolicy: archive.SourceRetentionVersion, Post: ingest.PostReference{Namespace: "native:reddit", Value: "root-post"},
		Source: []byte(`{"category":"reddit","id":"root-post","author":"Example","author_fullname":"t2_example"}`)}
	body, err := json.Marshal(event)
	require.NoError(t, err)
	receipt, err := service.Capture(t.Context(), token, body, ingest.Digest(body))
	require.NoError(t, err)
	raw := openRawDB(t, fixture.DatabasePath())
	defer raw.Close()
	for _, query := range []string{"UPDATE ingest_credential_roots SET root_uuid=root_uuid", "DELETE FROM ingest_credential_roots"} {
		_, err := raw.Exec(query)
		require.Error(t, err)
	}
	unbound := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Unbound", Kind: "feed", State: "active"}})
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: unbound.UUID, CollectionRevision: 1, CaptureUUID: receipt.CaptureUUID})
	}))
	bad := *receipt
	bad.EventUUID, bad.CollectionUUID, bad.RootUUID = uuid.NewString(), unbound.UUID, nil
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := repo.Ingest.RecordReceipt(ctx, bad); return err })
	require.ErrorContains(t, err, "outside its credential scope")
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(fixture, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	anonymous := openRawDB(t, output)
	defer anonymous.Close()
	for _, table := range []string{"ingest_receipts", "ingest_credentials", "ingest_credential_roots"} {
		require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM "+table))
	}
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM ingest_receipts"))
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM pragma_foreign_key_check"))
}
