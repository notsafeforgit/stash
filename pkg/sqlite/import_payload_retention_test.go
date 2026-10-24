package sqlite_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func completedPayloadFixture(t *testing.T, review bool, alter func([]map[string]any)) *catalogSnapshotFixture {
	t.Helper()
	f, binding := mediaImportFixture(t, 2, func(rows []map[string]any) {
		if review {
			return
		}
		// Use an account present in the real registry fixture. The unmodified
		// snapshot deliberately lacks this mapping and must remain retained.
		for _, row := range rows {
			values := row["values"].(map[string]any)
			if values["account_key"] == "reddit:handle:juniper" {
				values["account_key"] = "reddit:handle:deliberately-unlinked"
				if row["table"] == "accounts" {
					row["key"] = []any{"reddit:handle:deliberately-unlinked"}
				}
			}
		}
		if alter != nil {
			alter(rows)
		}
	})
	relations := advanceRelations(t, f, 0)
	for relations.State == "running" {
		relations = advanceRelations(t, f, relations.LastOrdinal)
	}
	pub := advancePublishers(t, f, 0)
	for pub.State == "running" {
		pub = advancePublishers(t, f, pub.LastOrdinal)
	}
	attachments := advanceAttachments(t, f, 0)
	for attachments.State == "running" {
		attachments = advanceAttachments(t, f, attachments.LastOrdinal)
	}
	finishMediaImport(t, f, beginMediaImport(t, f, binding))
	members := advanceMembershipImport(t, f, 0)
	for members.State == "running" {
		members = advanceMembershipImport(t, f, members.LastOrdinal)
	}
	finishDocuments(t, f)
	translations := advanceTranslations(t, f, 0)
	for translations.State == "running" {
		translations = advanceTranslations(t, f, translations.LastOrdinal)
	}
	enrichment := advanceCatalogEnrichment(t, f, 0)
	for enrichment.State == "running" {
		enrichment = advanceCatalogEnrichment(t, f, enrichment.LastOrdinal)
	}
	history := advanceFileHistory(t, f, 0)
	for history.State == "running" {
		history = advanceFileHistory(t, f, history.LastOrdinal)
	}
	cleanup := advanceCatalogCleanup(t, f, 0)
	for cleanup.State == "running" {
		cleanup = advanceCatalogCleanup(t, f, cleanup.LastOrdinal)
	}

	return f
}

func payloadPlan(t *testing.T, f *catalogSnapshotFixture) *models.ImportPayloadRetention {
	t.Helper()
	var result *models.ImportPayloadRetention
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.ArchiveImport.PayloadRetention(ctx, "catalog", f.manifest.UUID)
		return err
	}))
	return result
}

func retirePayloadBatch(t *testing.T, f *catalogSnapshotFixture, after int64, limit int) *models.ImportPayloadRetirement {
	t.Helper()
	var result *models.ImportPayloadRetirement
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.ArchiveImport.RetirePayloads(ctx, f.manifest.UUID, after, limit, catalogImportNow.Add(12*time.Hour))
		return err
	}))
	return result
}

func TestImportPayloadRetirementResumesPreservesReceiptsAndNativeData(t *testing.T) {
	f := completedPayloadFixture(t, false, nil)
	plan := payloadPlan(t, f)
	require.True(t, plan.Eligible, "%+v", plan)
	require.Positive(t, plan.CandidateRecords)
	raw := openRawDB(t, f.db.DatabasePath())
	before := map[string][][]any{}
	for _, table := range []string{"source_captures", "source_post_revisions", "source_payloads", "source_profile_bodies", "source_content_claims", "source_file_observations", "source_post_file_evidence", "source_media_evidence", "catalog_snapshot_chunks", "catalog_snapshot_tables", "catalog_evidence_records", "catalog_media_records", "scenes", "images", "galleries", "files"} {
		before[table] = albumJobRows(t, raw, table)
	}
	progress := retirePayloadBatch(t, f, 0, 1)
	require.EqualValues(t, 1, progress.RetiredRecords)
	require.Equal(t, progress, retirePayloadBatch(t, f, 0, 1), "retry of a committed batch returns its checkpoint")
	require.NoError(t, raw.Close())
	require.NoError(t, f.db.Close())
	_, err := sqlite.VerifyNativeSnapshot(t.Context(), f.db.DatabasePath())
	require.NoError(t, err, "an interrupted compaction remains a valid native backup")
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	for progress.State != "complete" {
		progress = retirePayloadBatch(t, f, progress.LastOrdinal, 2)
	}
	require.Equal(t, plan.CandidateRecords, progress.RetiredRecords)
	require.Equal(t, plan.CandidateBytes, progress.ReleasedBytes)
	require.Equal(t, progress, retirePayloadBatch(t, f, progress.LastOrdinal, 2))
	raw = openRawDB(t, f.db.DatabasePath())
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.Positive(t, queryUint(t, raw, `SELECT count(*) FROM catalog_snapshot_records r JOIN catalog_media_records m USING(snapshot_uuid,ordinal) WHERE m.outcome='unavailable' AND r.data<>'{}' AND r.original_byte_count IS NULL`))
	_, err = raw.Exec("UPDATE catalog_snapshot_records SET data='{}',byte_count=2 WHERE original_byte_count IS NULL")
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("UPDATE catalog_snapshot_records SET data_sha256=printf('%064d',0) WHERE original_byte_count IS NOT NULL")
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("UPDATE catalog_snapshot_records SET retained_asset_id='different' WHERE original_byte_count IS NOT NULL")
	require.ErrorContains(t, err, "immutable")
	for i := range f.chunks {
		require.Equal(t, "received", f.receive(t, i).State)
	}
	changed := append([]byte(nil), f.chunk(t, 0)...)
	changed[0] = ' '
	require.Error(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogSnapshot.Receive(ctx, f.manifest.UUID, f.sha, 0, changed, catalogImportNow.Add(time.Hour))
		return err
	}))
	require.NoError(t, raw.Close())
	require.NoError(t, f.db.Close())
	_, err = sqlite.VerifyNativeSnapshot(t.Context(), f.db.DatabasePath())
	require.NoError(t, err)
}

func TestImportPayloadRetirementRetainsIncompleteInputs(t *testing.T) {
	f, _ := mediaImportFixture(t, 1, nil)
	plan := payloadPlan(t, f)
	require.False(t, plan.Eligible)
	require.Equal(t, "incomplete_imports_or_historical_reviews", plan.Reason)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveImport.RetirePayloads(ctx, f.manifest.UUID, 0, 512, catalogImportNow.Add(12*time.Hour))
		return err
	})
	require.ErrorIs(t, err, models.ErrImportPayloadRetentionConflict)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_payload_retirements"))
}

func TestImportPayloadRetirementAcceptsCompactedNFOStaging(t *testing.T) {
	f := completedPayloadFixture(t, false, func(rows []map[string]any) {
		for _, row := range rows {
			table := row["table"].(string)
			values := row["values"].(map[string]any)
			if table == "observations" && values["origin"] == "legacy-nfo" {
				values["title"] = "Original title"
				values["published_at"] = nil
				values["date_basis"] = nil
			}
			if strings.HasPrefix(table, "sidecar") {
				values["content_sha256"] = scrape.CatalogSnapshotSHA(nil)
				switch table {
				case "sidecar_documents":
					values["raw_content"] = map[string]any{"sqlite_blob_base64": ""}
					values["parse_status"] = "empty"
					values["warnings_json"] = "[]"
				case "sidecar_sources":
					row["key"] = []any{values["relpath"], values["content_sha256"]}
				}
			}
		}
	})
	result, err := f.db.CompactImportedNFO(t.Context(), nil)
	require.NoError(t, err)
	require.Positive(t, result.StagingRows)
	plan := payloadPlan(t, f)
	require.True(t, plan.Eligible)
	raw := openRawDB(t, f.db.DatabasePath())
	require.Positive(t, queryUint(t, raw, `SELECT count(*) FROM catalog_retirable_payloads p
JOIN catalog_snapshot_records r USING(snapshot_uuid,ordinal) WHERE substr(r.data,-1)<>char(10)`),
		"exercise JSON objects rewritten by historical NFO compaction")
	before := map[string][][]any{}
	for _, table := range []string{"source_captures", "source_post_revisions", "catalog_snapshot_chunks", "catalog_evidence_records", "scenes", "images", "galleries"} {
		before[table] = albumJobRows(t, raw, table)
	}
	progress := retirePayloadBatch(t, f, 0, 2)
	for progress.State != "complete" {
		progress = retirePayloadBatch(t, f, progress.LastOrdinal, 2)
	}
	require.Equal(t, plan.CandidateRecords, progress.RetiredRecords)
	require.Equal(t, plan.CandidateBytes, progress.ReleasedBytes)
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	for i := range f.chunks {
		require.Equal(t, "received", f.receive(t, i).State)
	}
	require.NoError(t, raw.Close())
	require.NoError(t, f.db.Close())
	_, err = sqlite.VerifyNativeSnapshot(t.Context(), f.db.DatabasePath())
	require.NoError(t, err)
}

func TestImportPayloadRetirementRetainsHistoricalReviews(t *testing.T) {
	f := completedPayloadFixture(t, true, nil)
	plan := payloadPlan(t, f)
	require.False(t, plan.Eligible)
	require.Equal(t, "incomplete_imports_or_historical_reviews", plan.Reason)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveImport.RetirePayloads(ctx, f.manifest.UUID, 0, 512, catalogImportNow.Add(12*time.Hour))
		return err
	})
	require.ErrorIs(t, err, models.ErrImportPayloadRetentionConflict)
}

func TestImportPayloadRetirementFailureCannotCommitPartialBatch(t *testing.T) {
	f := completedPayloadFixture(t, false, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`CREATE TRIGGER reject_retirement BEFORE UPDATE OF original_byte_count ON catalog_snapshot_records
WHEN (SELECT count(*) FROM catalog_snapshot_records WHERE original_byte_count IS NOT NULL)>0
BEGIN SELECT RAISE(ABORT,'injected retirement failure'); END`)
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveImport.RetirePayloads(ctx, f.manifest.UUID, 0, 512, catalogImportNow.Add(12*time.Hour))
		require.ErrorContains(t, err, "injected retirement failure")
		return nil // Even a caller swallowing the error must not publish half a batch.
	})
	require.ErrorContains(t, err, "did not finish atomically")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_snapshot_records WHERE original_byte_count IS NOT NULL"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_payload_retirements"))
	_, err = raw.Exec("DROP TRIGGER reject_retirement")
	require.NoError(t, err)
	require.Equal(t, "complete", retirePayloadBatch(t, f, 0, 512).State)
}
