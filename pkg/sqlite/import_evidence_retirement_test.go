package sqlite_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

var retiredImportEvidence = []string{"source_enrichment_receipts", "source_file_history", "source_file_history_locations",
	"source_file_history_deduplications", "source_file_history_edits", "source_file_history_states",
	"metadata_file_edit_reviews", "metadata_file_edit_keeps", "enrichment_completion_legacy_receipt"}

// Schema 1000144 drops the frozen receipts and file history. A completion that
// cited a receipt keeps the UUID its digest covers and still reads back.
func TestImportEvidenceRetirementKeepsReceiptCompletionsAndDropsFileHistory(t *testing.T) {
	f := newLookupExecutionFixture(t, nil)
	other := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:" + lookupCatalog, Value: "legacy:post:receipt"}, "")
	otherURL, err := observePostURL(f.repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(other.UUID), URL: "https://www.reddit.com/comments/receipt"})
	require.NoError(t, err)
	var root *models.MediaRoot
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		root, err = f.repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "migration", MediaRootDefinition: models.MediaRootDefinition{Label: "Old root", State: "disabled"}})
		return err
	}))
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeImportEvidenceSchema(t, raw)
	_, err = raw.Exec("UPDATE schema_migrations SET version=1000143,dirty=0")
	require.NoError(t, err)
	for _, name := range retiredImportEvidence {
		require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='"+name+"'"), name)
	}

	// Frozen evidence at 1000143: one receipt-backed completion of a migrated
	// target, and one file history record with its location.
	abs, err := filepath.Abs(path)
	require.NoError(t, err)
	unchecked, err := sql.Open("sqlite3ex", (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: "_fk=false"}).String())
	require.NoError(t, err)
	defer unchecked.Close()
	unchecked.SetMaxOpenConns(1)
	var guards []string
	for _, name := range []string{"source_enrichment_receipt_scope", "enrichment_completion_scope", "source_file_history_location_scope"} {
		var definition string
		require.NoError(t, unchecked.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", name).Scan(&definition))
		_, err = unchecked.Exec("DROP TRIGGER " + name)
		require.NoError(t, err)
		guards = append(guards, definition)
	}
	target, err := archive.EnrichmentTargetIdentity(models.EnrichmentTargetInput{PostUUID: other.UUID, URLUUID: otherURL.URLUUID,
		CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, Policy: models.EnrichmentGalleryMetadataV1, Origin: "migration"})
	require.NoError(t, err)
	_, err = unchecked.Exec(`INSERT INTO enrichment_targets(uuid,post_uuid,url_uuid,collection_uuid,collection_revision,policy,origin,state,priority,not_before,reason,created_at,updated_at)
 VALUES(?,?,?,?,?,'gallery-dl-metadata-v1','migration','held',10,?,'',?,?)`, target, other.UUID, otherURL.URLUUID, f.collection.UUID, f.collection.Revision, f.now, f.now, f.now)
	require.NoError(t, err)
	receipt, completion := uuid.NewString(), uuid.NewString()
	_, err = unchecked.Exec(`INSERT INTO source_enrichment_receipts(uuid,post_uuid,collection_uuid,collection_revision,origin,policy,source_version,completed_at,attachment_links_enriched,unresolved_children,recorded_at)
 VALUES(?,?,?,1,'migration','catalog-enrichment-v1',1,'2026-10-02T01:39:06.729089+00:00',1,0,'2026-10-08T19:56:13.103984249Z')`, receipt, other.UUID, f.collection.UUID)
	require.NoError(t, err)
	proof, err := archive.EncodeSourceJSON(struct {
		UUID             string  `json:"uuid"`
		TargetUUID       string  `json:"target_uuid"`
		ExpectedRevision int     `json:"expected_revision"`
		ReceiptUUID      *string `json:"receipt_uuid"`
		CaptureUUID      *string `json:"capture_uuid"`
	}{completion, target, 1, &receipt, nil})
	require.NoError(t, err)
	digest := sha256.Sum256(append([]byte("stash-imported-enrichment-completion-v1\x00"), proof...))
	_, err = unchecked.Exec(`INSERT INTO enrichment_completions(uuid,target_uuid,expected_revision,request_digest,capture_count,legacy_receipt_uuid,created_at)
 VALUES(?,?,1,?,0,?,?)`, completion, target, hex.EncodeToString(digest[:]), receipt, f.now)
	require.NoError(t, err)
	_, err = unchecked.Exec("UPDATE enrichment_targets SET state='completed',completion_uuid=?,revision=2,updated_at=? WHERE uuid=?", completion, f.now, target)
	require.NoError(t, err)
	history := uuid.NewString()
	_, err = unchecked.Exec(`INSERT INTO source_file_history(uuid,kind,collection_uuid,collection_revision,root_uuid,root_revision,reference_namespace,reference_value,source_time,origin,observed_at,details,location_count,edit_count,signature)
 VALUES(?,'state_change',?,?,?,?,'fixture:catalog','state','','migration',?,'{}',1,0,?)`, history, f.collection.UUID, f.collection.Revision, root.UUID, root.Revision, f.now, strings.Repeat("a", 64))
	require.NoError(t, err)
	_, err = unchecked.Exec("INSERT INTO source_file_history_locations(history_uuid,position,relative_path) VALUES(?,0,'clip.mp4')", history)
	require.NoError(t, err)
	// Restore the guards so the 1000143 snapshot is complete before upgrading.
	for _, definition := range guards {
		_, err = unchecked.Exec(definition)
		require.NoError(t, err)
	}
	completions := albumJobRows(t, raw, "enrichment_completions")

	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, f.db.Open(path), &needed)
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.Open(path))
	require.NoError(t, f.db.AuditForTesting(path))
	f.repo = f.db.Repository()

	for _, name := range retiredImportEvidence {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='"+name+"'"), name)
	}
	require.Equal(t, completions, albumJobRows(t, raw, "enrichment_completions"), "every completion row is retained exactly")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_list('enrichment_completions') WHERE \"table\"='source_enrichment_receipts'"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	var details string
	require.NoError(t, raw.QueryRow("SELECT details FROM native_migration_history WHERE version=1000144").Scan(&details))
	require.JSONEq(t, `{"enrichment_receipts":1,"receipt_completions":1,"file_histories":1,"file_history_locations":1,
 "file_history_deduplications":0,"file_history_edits":0,"file_history_states":0,"file_edit_reviews":0,"file_edit_keeps":0}`, details)
	var read *models.EnrichmentCompletion
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		read, err = f.repo.EnrichmentWork.Completion(ctx, completion)
		return err
	}))
	require.Equal(t, "legacy_receipt", read.Basis)
	require.Equal(t, &receipt, read.LegacyReceiptUUID)

	// Nothing can cite a receipt any more.
	_, err = raw.Exec(`INSERT INTO enrichment_completions(uuid,target_uuid,expected_revision,request_digest,capture_count,legacy_receipt_uuid,created_at)
 VALUES(?,?,?,?,0,?,?)`, uuid.NewString(), f.target.UUID, f.target.Revision, strings.Repeat("b", 64), uuid.NewString(), f.now)
	require.ErrorContains(t, err, "enrichment completion requires its exact target revision and proof scope")
	_, err = raw.Exec("UPDATE enrichment_completions SET legacy_receipt_uuid=NULL WHERE uuid=?", completion)
	require.ErrorContains(t, err, "enrichment completion evidence is immutable")
}
