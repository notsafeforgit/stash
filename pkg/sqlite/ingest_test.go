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

func TestIngestReceiptConstraintsAndAnonymisation(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Private intake", Kind: "feed", State: "active", Namespace: "native:reddit"}})
	var producer *models.IngestProducer
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		producer, err = repo.Ingest.CreateProducer(ctx, "Private worker")
		return err
	}))
	service := ingest.New(repo)
	credential, token, err := service.IssueCredential(context.Background(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID}}, nil)
	require.NoError(t, err)
	event := ingest.CaptureEvent{Protocol: 1, ProducerUUID: producer.UUID, EventUUID: uuid.NewString(), RunUUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, Kind: "source.capture", ObservedAt: time.Now().UTC(), ExtractorVersion: "fixture", RetentionPolicy: archive.SourceRetentionVersion, Post: ingest.PostReference{Namespace: "native:reddit", Value: "post-one"}, Source: []byte(`{"category":"reddit","id":"post-one","author":"Account","author_fullname":"t2_account"}`)}
	body, err := json.Marshal(event)
	require.NoError(t, err)
	receipt, err := service.Capture(context.Background(), token, body, ingest.Digest(body))
	require.NoError(t, err)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	for _, query := range []string{
		"UPDATE ingest_receipts SET result='{}'",
		"UPDATE ingest_credentials SET secret_hash='0000000000000000000000000000000000000000000000000000000000000000'",
		"UPDATE ingest_producers SET label='changed identity'",
		"UPDATE ingest_credential_scopes SET collection_uuid=collection_uuid",
		"DELETE FROM ingest_credential_scopes",
	} {
		_, err := raw.Exec(query)
		require.Error(t, err, query)
	}
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.Ingest.RevokeCredential(ctx, credential.UUID) }))
	_, err = raw.Exec("UPDATE ingest_credentials SET revoked=0")
	require.ErrorContains(t, err, "only be revoked")
	// Incorrect collection/capture provenance cannot receive a durable receipt.
	bad := *receipt
	bad.EventUUID = uuid.NewString()
	bad.CollectionRevision++
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error { _, err := repo.Ingest.RecordReceipt(ctx, bad); return err })
	require.Error(t, err)
	other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "different"}, "")
	bad = *receipt
	bad.EventUUID = uuid.NewString()
	bad.PostUUID = other.UUID
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error { _, err := repo.Ingest.RecordReceipt(ctx, bad); return err })
	require.Error(t, err)
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM ingest_receipts"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	rows, err := raw.Query("EXPLAIN QUERY PLAN SELECT * FROM ingest_receipts WHERE producer_uuid=? AND event_uuid=?", producer.UUID, event.EventUUID)
	require.NoError(t, err)
	defer rows.Close()
	var details string
	for rows.Next() {
		var a, b, c int
		var detail string
		require.NoError(t, rows.Scan(&a, &b, &c, &detail))
		details += detail
	}
	require.NoError(t, rows.Err())
	require.Contains(t, details, "SEARCH ingest_receipts")
	require.NotContains(t, details, "SCAN ingest_receipts")
	out := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anon, err := sqlite.NewAnonymiser(db, out)
	require.NoError(t, err)
	require.NoError(t, anon.Anonymise(context.Background()))
	anonymous := openRawDB(t, out)
	defer anonymous.Close()
	for _, table := range []string{"ingest_receipts", "ingest_credential_scopes", "ingest_credentials", "ingest_producers"} {
		require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestIngestMigrationRetainsPopulatedCaptureAndPublisher(t *testing.T) {
	fixture, repo := archiveTestDatabase(t)
	capture := publisherCapture(t, repo, "native:twitter", "twitter", `{"category":"twitter","tweet_id":"kept-post","author":{"id":"author","name":"Example"}}`)
	decision, err := applyPublisher(repo, publisherInput(publisherPreview(t, repo, capture.UUID, ""), "automatic"))
	require.NoError(t, err)
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Retained feed", Kind: "feed", State: "active"}})
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		return repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CaptureUUID: capture.UUID, CollectionUUID: collection.UUID, CollectionRevision: 1})
	}))
	path := filepath.Join(t.TempDir(), "schema16.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+16; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	_, err = raw.Exec("ATTACH DATABASE ? AS source_fixture", fixture.DatabasePath())
	require.NoError(t, err)
	tx, err := raw.Begin()
	require.NoError(t, err)
	for _, table := range []string{"source_accounts", "source_account_identifiers", "source_account_identifier_evidence", "source_collections", "source_collection_revisions", "source_posts", "source_post_identifiers", "source_payloads", "source_profile_bodies", "source_post_revisions", "source_captures", "source_capture_profiles", "source_collection_captures", "capture_publisher_decisions", "capture_publisher_claims"} {
		_, err := tx.Exec("INSERT INTO " + table + " SELECT * FROM source_fixture." + table)
		require.NoError(t, err, table)
	}
	require.NoError(t, tx.Commit())
	_, err = raw.Exec("DETACH DATABASE source_fixture")
	require.NoError(t, err)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='ingest_receipts'"))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	upgraded := db.Repository()
	require.NoError(t, upgraded.WithReadTxn(context.Background(), func(ctx context.Context) error {
		retained, err := upgraded.SourceEvidence.FindCapture(ctx, capture.UUID)
		require.NoError(t, err)
		require.Equal(t, capture, retained)
		publisher, err := upgraded.CapturePublisher.Current(ctx, capture.UUID)
		require.NoError(t, err)
		require.Equal(t, decision, publisher)
		kept, err := upgraded.SourceCollection.Find(ctx, collection.UUID)
		require.NoError(t, err)
		require.Equal(t, collection, kept)
		return nil
	}))
	for _, table := range []string{"ingest_receipts", "ingest_credential_scopes", "ingest_credentials", "ingest_producers"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
