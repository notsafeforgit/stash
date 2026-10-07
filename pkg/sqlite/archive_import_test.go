package sqlite_test

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

func TestArchiveImportHistorySeparatesTransferFromDomainProgress(t *testing.T) {
	f := snapshotFixture(t)
	start := f.begin(t)
	read := func() *models.ArchiveImportDetails {
		t.Helper()
		var result *models.ArchiveImportDetails
		require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			result, err = f.repo.ArchiveImport.Snapshot(ctx, "catalog", start.UUID)
			return err
		}))
		return result
	}
	initial := read()
	require.Equal(t, "receiving", initial.Snapshot.TransferState)
	require.EqualValues(t, 0, initial.Snapshot.ReceivedRecords)
	require.Equal(t, start.CollectionUUID, *initial.Snapshot.CollectionUUID)
	require.NotEmpty(t, *initial.Snapshot.CollectionLabel)
	require.Len(t, initial.Families, 11)
	for _, family := range initial.Families {
		require.Nil(t, family.Progress)
	}
	for index := range f.manifest.Chunks {
		f.receive(t, index)
	}
	received := read()
	require.Equal(t, "received", received.Snapshot.TransferState)
	require.Equal(t, received.Snapshot.Records, received.Snapshot.ReceivedRecords)
	require.Equal(t, received.Snapshot.Bytes, received.Snapshot.ReceivedBytes)
	require.Equal(t, received.Snapshot.Chunks, received.Snapshot.ReceivedChunks)
	require.Equal(t, initial.Families, received.Families, "transferred bytes do not run any importers")
	progress := advanceEvidence(t, f, 0)
	mapped := read()
	require.Equal(t, received.Snapshot, mapped.Snapshot)
	require.Equal(t, "evidence", mapped.Families[0].Name)
	require.Equal(t, &models.ArchiveImportProgress{State: progress.State, SourceRecords: progress.TotalRecords,
		ProcessedRecords: progress.ProcessedRecords, HistoricalReviewRecords: progress.ReviewRecords, UpdatedAt: progress.UpdatedAt}, mapped.Families[0].Progress)
	for _, family := range mapped.Families[1:] {
		require.Nil(t, family.Progress)
	}
	encoded, err := json.Marshal(mapped)
	require.NoError(t, err)
	for _, forbidden := range []string{"payload", "manifest_sha256", "pending_families", "\"imported\"", "settings", "source_values"} {
		require.NotContains(t, string(encoded), forbidden)
	}
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, mapped, read(), "inspection and reopening do not change recorded progress")
}

func TestArchiveImportHistoryKeepsHistoricalReviewCounts(t *testing.T) {
	f := relationFixture(t, nil)
	progress := advanceRelations(t, f, 0)
	for progress.State == "running" {
		progress = advanceRelations(t, f, progress.LastOrdinal)
	}
	require.Positive(t, progress.ReviewRecords)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		detail, err := f.repo.ArchiveImport.Snapshot(ctx, "catalog", f.manifest.UUID)
		require.NoError(t, err)
		require.Equal(t, "relations", detail.Families[1].Name)
		require.Equal(t, progress.ReviewRecords, detail.Families[1].Progress.HistoricalReviewRecords)
		require.Equal(t, "review", detail.Families[1].Progress.State)
		encoded, err := json.Marshal(detail)
		require.NoError(t, err)
		require.Contains(t, string(encoded), "historical_review_records")
		require.NotContains(t, string(encoded), "pending")
		return nil
	}))
}

func TestArchiveImportHistoryPaginatesAndValidatesKinds(t *testing.T) {
	db, repo, registry, _ := registryFixture(t, func(d *registryTestDocument) {
		for _, id := range []string{"c_11111111111111111111111111111111", "c_22222222222222222222222222222222"} {
			d.Tables["catalogs"] = append(d.Tables["catalogs"], map[string]any{"id": id, "kind": "collection", "label": "Historical album", "owner_key": "directory:" + id, "created_at": "2025-01-01T00:00:00Z", "redirect_to": nil})
		}
	})
	registryApply(t, repo, registry, registryPreview(t, repo, registry))
	manifestBody, err := os.ReadFile("../scrape/testdata/catalog_snapshot/manifest.json")
	require.NoError(t, err)
	var manifestDocument map[string]any
	require.NoError(t, json.Unmarshal(manifestBody, &manifestDocument))
	manifestDocument["registry_source_uuid"] = registry.SourceUUID
	manifestBody, err = json.Marshal(manifestDocument)
	require.NoError(t, err)
	f := &catalogSnapshotFixture{db: db, repo: repo, body: manifestBody, sha: scrape.CatalogSnapshotSHA(manifestBody)}
	first := f.begin(t)
	var document map[string]any
	require.NoError(t, json.Unmarshal(f.body, &document))
	otherCatalog := "c_22222222222222222222222222222222"
	secondID := uuid.NewString()
	document["snapshot_uuid"], document["catalog_id"] = secondID, otherCatalog
	document["catalog_info"].(map[string]any)["id"] = otherCatalog
	body, err := json.Marshal(document)
	require.NoError(t, err)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogSnapshot.Begin(ctx, body, scrape.CatalogSnapshotSHA(body), catalogImportNow)
		return err
	}))
	expected := []string{first.UUID, secondID}
	slices.Sort(expected)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var actual []string
		after := ""
		for range 3 {
			page, err := f.repo.ArchiveImport.Snapshots(ctx, models.ArchiveImportFilter{Kind: "catalog", After: after, Limit: 1})
			require.NoError(t, err)
			if len(page) == 0 {
				break
			}
			require.Len(t, page, 1)
			actual = append(actual, page[0].UUID)
			after = page[0].UUID
		}
		require.Equal(t, expected, actual)
		empty, err := f.repo.ArchiveImport.Snapshots(ctx, models.ArchiveImportFilter{Kind: "automation", Limit: 25})
		require.NoError(t, err)
		require.NotNil(t, empty)
		require.Empty(t, empty)
		for _, filter := range []models.ArchiveImportFilter{
			{Kind: "bad", Limit: 1}, {Kind: "catalog"}, {Kind: "automation", Limit: 101},
			{Kind: "catalog", Limit: 1, After: "bad"}, {Kind: "catalog", Limit: -1},
		} {
			_, err := f.repo.ArchiveImport.Snapshots(ctx, filter)
			require.ErrorIs(t, err, models.ErrArchiveImportInvalid)
		}
		missing, err := f.repo.ArchiveImport.Snapshot(ctx, "catalog", uuid.NewString())
		require.NoError(t, err)
		require.Nil(t, missing)
		_, err = f.repo.ArchiveImport.Snapshot(ctx, "catalog", "bad")
		require.ErrorIs(t, err, models.ErrArchiveImportInvalid)
		_, err = f.repo.ArchiveImport.Snapshot(ctx, "bad", first.UUID)
		require.ErrorIs(t, err, models.ErrArchiveImportInvalid)
		return nil
	}))
}

func TestArchiveImportHistoryIncludesEmptyAutomationWithoutInventingWork(t *testing.T) {
	f := automationFixture(t, true)
	start := f.begin(t)
	require.Equal(t, "received", start.State)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.AutomationTranslationImport.Advance(ctx, start.UUID, f.sha, 0, automationImportNow.Add(time.Minute))
		return err
	}))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		list, err := f.repo.ArchiveImport.Snapshots(ctx, models.ArchiveImportFilter{Kind: "automation", Limit: 25})
		require.NoError(t, err)
		require.Len(t, list, 1)
		detail, err := f.repo.ArchiveImport.Snapshot(ctx, "automation", start.UUID)
		require.NoError(t, err)
		require.Equal(t, list[0], detail.Snapshot)
		require.Nil(t, detail.Snapshot.CollectionUUID)
		require.Nil(t, detail.Snapshot.CollectionLabel)
		require.Zero(t, detail.Snapshot.Records)
		require.Len(t, detail.Families, 4)
		require.NotNil(t, detail.Families[0].Progress)
		require.Zero(t, detail.Families[0].Progress.SourceRecords)
		for _, family := range detail.Families[1:] {
			require.Nil(t, family.Progress)
		}
		jobs, err := f.repo.ArchiveActivity.Jobs(ctx, models.ArchiveJobActivityFilter{ArchiveActivityPage: models.ArchiveActivityPage{Limit: 1}})
		require.NoError(t, err)
		require.Empty(t, jobs)
		return nil
	}))
}
