package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeAttachmentDownloadSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	var exists bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='source_attachment_downloads')").Scan(&exists))
	if !exists {
		return
	}
	_, err := raw.Exec("DROP TRIGGER attachment_download_receipt; DROP TABLE source_attachment_downloads; DELETE FROM native_migration_history WHERE version=1000087")
	require.NoError(t, err)
	// Restore the actual pre-87 receipt constraints, preserving its data.
	prior, err := os.ReadFile("migrations/1000024_ingest_root_grants.up.sql")
	require.NoError(t, err)
	body := string(prior)
	body = body[strings.Index(body, "CREATE TABLE ingest_receipts_next"):strings.Index(body, "INSERT INTO native_migration_history")]
	_, err = raw.Exec("DROP TRIGGER ingest_scope_receipts; DROP TRIGGER ingest_root_receipts;" + body)
	require.NoError(t, err)
}

func downloadSQLFixture(t *testing.T) (*sourceRunFixture, ingest.AttachmentDownloadEvent, models.AttachmentDownloadInput) {
	t.Helper()
	f := newSourceRunFixture(t)
	run := f.claim(t, f.submit(t, f.request()))
	capture := ingest.CaptureEvent{Protocol: 1, ProducerUUID: f.producer.UUID, EventUUID: uuid.NewString(), RunUUID: run.UUID,
		CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RootUUID: &f.root.UUID, Kind: "source.capture", ObservedAt: f.now,
		ExtractorVersion: "fixture", RetentionPolicy: archive.SourceRetentionVersion, Post: ingest.PostReference{Namespace: "native:reddit", Value: "download-post"},
		Source: []byte(`{"category":"reddit","id":"download-post","url":"https://i.redd.it/first.jpg"}`)}
	body, err := json.Marshal(capture)
	require.NoError(t, err)
	receipt, err := f.service.Capture(t.Context(), f.token, body, ingest.Digest(body))
	require.NoError(t, err)
	var attachment *models.SourceAttachment
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		attachment, err = f.repo.SourceAttachment.Lookup(ctx, receipt.PostUUID, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "first"})
		return err
	}))
	require.NotNil(t, attachment)
	event := ingest.AttachmentDownloadEvent{Protocol: 1, ProducerUUID: f.producer.UUID, EventUUID: uuid.NewString(), RunUUID: run.UUID,
		CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RootUUID: f.root.UUID, Kind: "attachment.download", ObservedAt: f.now,
		OwnerUUID: run.OwnerUUID, Fence: run.Fence, TransferSequence: 17, CaptureEventUUID: capture.EventUUID,
		Attachment: ingest.PostReference{Namespace: attachment.Reference.Namespace, Value: attachment.Reference.Value}, State: "started"}
	input := models.AttachmentDownloadInput{ProducerUUID: event.ProducerUUID, EventUUID: event.EventUUID, RunUUID: event.RunUUID,
		Fence: event.Fence, OwnerUUID: event.OwnerUUID, TransferSequence: event.TransferSequence, CaptureEventUUID: event.CaptureEventUUID,
		AttachmentUUID: attachment.UUID, State: event.State, ObservedAt: event.ObservedAt}
	return f, event, input
}

func TestAttachmentDownloadSchemaRequiresAtomicReceiptAndIndexedHistory(t *testing.T) {
	f, event, input := downloadSQLFixture(t)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		require.NoError(t, f.repo.SourceAttachment.RecordDownload(ctx, input))
		return nil // A caught or omitted receipt cannot commit the report alone.
	})
	require.ErrorIs(t, err, models.ErrAttachmentDownloadInvalid)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_attachment_downloads"))
	body, err := json.Marshal(event)
	require.NoError(t, err)
	_, err = f.service.AttachmentDownload(t.Context(), f.token, body, ingest.Digest(body))
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE source_attachment_downloads SET reason_code=reason_code")
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("DELETE FROM ingest_receipts WHERE kind='attachment.download'")
	require.Error(t, err, "a committed report keeps its original acknowledgement")
	query, err := raw.Query("EXPLAIN QUERY PLAN SELECT * FROM source_attachment_downloads WHERE attachment_uuid=? AND id>? ORDER BY id LIMIT ?", input.AttachmentUUID, 0, 25)
	require.NoError(t, err)
	defer query.Close()
	var details string
	for query.Next() {
		var a, b, c int
		var detail string
		require.NoError(t, query.Scan(&a, &b, &c, &detail))
		details += detail
	}
	require.NoError(t, query.Err())
	require.Contains(t, details, "SEARCH source_attachment_downloads USING INDEX attachment_download_history")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestAttachmentDownloadSchemaAnonymisesAndRejectsCorruptScopeBeforeWrites(t *testing.T) {
	f, event, _ := downloadSQLFixture(t)
	body, err := json.Marshal(event)
	require.NoError(t, err)
	_, err = f.service.AttachmentDownload(t.Context(), f.token, body, ingest.Digest(body))
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anon, err := sqlite.NewAnonymiser(f.db, path)
	require.NoError(t, err)
	require.NoError(t, anon.Anonymise(t.Context()))
	anonymous := openRawDB(t, path)
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM source_attachment_downloads"))
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, anonymous.Close())
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	var trigger string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='attachment_download_immutable'").Scan(&trigger))
	_, err = raw.Exec("DROP TRIGGER attachment_download_immutable; UPDATE source_attachment_downloads SET owner_uuid=?", uuid.NewString())
	require.NoError(t, err)
	_, err = raw.Exec(trigger)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.ErrorContains(t, f.db.Open(f.db.DatabasePath()), "inconsistent attachment download")
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestAttachmentDownloadMigrationPreservesOriginalReceiptsAndOwnership(t *testing.T) {
	f, _, _ := downloadSQLFixture(t)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	tables := []string{"ingest_receipts", "source_runs", "source_run_attempts", "source_captures", "source_attachment_entries"}
	before := map[string][][]any{}
	for _, table := range tables {
		before[table] = albumJobRows(t, raw, table)
	}
	removeAttachmentDownloadSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000086,dirty=0")
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.ReInitialise())
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	for _, table := range tables {
		require.Equal(t, before[table], albumJobRows(t, raw, table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_attachment_downloads"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
