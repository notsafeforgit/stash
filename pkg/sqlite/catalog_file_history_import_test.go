package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func fileHistoryImportFixture(t *testing.T, extra int, alter func([]map[string]any)) *catalogSnapshotFixture {
	t.Helper()
	f := snapshotFixture(t)
	rows := documentImportRows(t, f)
	for table, columns := range map[string]string{
		"metadata_edits": "edit_id relpath fields_json created_at", "file_events": "event_id relpath old_state new_state reason observed_at",
	} {
		names := strings.Fields(columns)
		definition := scrape.CatalogSnapshotTable{Key: names[:1]}
		ddl := []string{}
		for i, name := range names {
			column := scrape.CatalogSnapshotColumn{CID: i, Name: name, Type: "TEXT", NotNull: 1, Default: json.RawMessage(`null`)}
			if i == 0 {
				column.PK = 1
			}
			definition.Columns = append(definition.Columns, column)
			ddl = append(ddl, name+" TEXT NOT NULL")
		}
		f.manifest.Tables[table] = definition
		entry := f.manifest.Schema[0]
		entry.Type, entry.Name, entry.Table = "table", table, table
		entry.SQL = "CREATE TABLE " + table + "(" + strings.Join(ddl, ",") + ",PRIMARY KEY(" + names[0] + "))"
		f.manifest.Schema = append(f.manifest.Schema, entry)
		f.manifest.References[table+".relpath"] = 0
	}
	add := func(table string, values map[string]any) {
		rows = append(rows, map[string]any{"table": table, "key": []any{values[f.manifest.Tables[table].Key[0]]}, "values": values})
	}
	stamp := "2026-09-28T01:02:03.123456789-07:00"
	add("assets", map[string]any{"asset_id": "fclones:blake3:unverified:1000", "digest_algorithm": "blake3", "digest": "unverified", "byte_size": 1000, "created_at": stamp})
	for i, path := range []string{"video.mp4", "old.mp4"} {
		state, survivor := "present", any(nil)
		if i == 1 {
			state, survivor = "deduplicated", "video.mp4"
		}
		add("files", map[string]any{"relpath": path, "asset_id": "fclones:blake3:unverified:1000", "state": state, "byte_size": 1000,
			"mtime_ns": nil, "first_observed": stamp, "survivor_relpath": survivor, "role": "local"})
	}
	add("dedupe_events", map[string]any{"event_id": "shared-event-id", "asset_id": "fclones:blake3:unverified:1000", "paths_json": `["old.mp4","video.mp4"]`, "survivor_relpath": "video.mp4", "stage": "finished", "created_at": stamp})
	add("file_events", map[string]any{"event_id": "state", "relpath": "old.mp4", "old_state": "present", "new_state": "deduplicated", "reason": "interrupted-dedupe-reconciliation", "observed_at": stamp})
	for i, input := range []struct{ path, fields, stamp string }{
		{"video.mp4", `{"title":"Exact path title","date":"2026-02-28","actors":["Ambiguous name"],"urls":["https://example.test/post"]}`, stamp},
		{"old.mp4", `{"title":"Later duplicate title","movie":"Purchased set","tags":["Example"],"studio":"Publisher"}`, "2026-09-28T02:00:00Z"},
		{"video.mp4", `{"title":null,"future":{"source_id":9007199254740993123456789},"rating":95,"details":"Exact \\n <&>\u2028"}`, stamp},
	} {
		add("metadata_edits", map[string]any{"edit_id": fmt.Sprintf("edit-%03d", i), "relpath": input.path, "fields_json": input.fields, "created_at": input.stamp})
	}
	for i := range extra {
		add("metadata_edits", map[string]any{"edit_id": fmt.Sprintf("more-%03d", i), "relpath": "video.mp4", "fields_json": `{"details":"Historical choice"}`, "created_at": ""})
	}
	if alter != nil {
		alter(rows)
	}
	receiveCatalogFixtureRows(t, f, rows)
	finishMediaImport(t, f, beginMediaImport(t, f, bindCatalogMediaFixture(t, f)))
	return f
}

func advanceFileHistory(t *testing.T, f *catalogSnapshotFixture, after int64) *models.CatalogFileHistoryImport {
	t.Helper()
	var ret *models.CatalogFileHistoryImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = f.repo.CatalogFileHistoryImport.Advance(ctx, f.manifest.UUID, f.sha, after, catalogImportNow.Add(9*time.Hour))
		return err
	}))
	return ret
}

func TestCatalogFileHistoryPreservesEditsInheritanceAndDedupeWithoutApplyingThem(t *testing.T) {
	f := fileHistoryImportFixture(t, 51, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "galleries", "files", "performers", "performers_scenes", "performers_images", "metadata_field_decisions", "source_content_claims", "source_file_observations", "source_file_matches", "source_posts"} {
		before[table] = albumJobRows(t, raw, table)
	}
	first := advanceFileHistory(t, f, 0)
	require.Equal(t, "running", first.State)
	require.EqualValues(t, 50, first.ProcessedRecords)
	require.Greater(t, first.LastOrdinal, first.ProcessedRecords)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogFileHistoryImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		return err
	})
	require.ErrorIs(t, err, models.ErrCatalogSnapshotConflict)
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: first.CollectionUUID, ExpectedRevision: 1, Origin: "review",
		SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Later renamed collection", Kind: "directory", State: "retired"}})
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	result := advanceFileHistory(t, f, first.LastOrdinal)
	require.Equal(t, "review", result.State)
	require.EqualValues(t, 55, result.MappedRecords)
	require.EqualValues(t, 1, result.ReviewRecords)
	require.Equal(t, 1, result.CollectionRevision)
	require.False(t, result.Imported)
	require.Equal(t, result, advanceFileHistory(t, f, result.LastOrdinal))
	for table, values := range before {
		require.Equal(t, values, albumJobRows(t, raw, table), table)
	}
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		page, err := f.repo.CatalogFileHistoryImport.Records(ctx, f.manifest.UUID, 0, 1)
		require.NoError(t, err)
		require.Len(t, page, 1)
		dedupe, err := f.repo.SourceFileHistory.Find(ctx, *page[0].HistoryUUID)
		require.NoError(t, err)
		require.Equal(t, "shared-event-id", dedupe.ReferenceValue)
		require.Equal(t, "2026-09-28T01:02:03.123456789-07:00", dedupe.SourceTime)
		require.Equal(t, "finished", dedupe.Deduplication.Stage)
		require.Equal(t, "old.mp4", dedupe.Locations[0].RelativePath)
		require.Equal(t, "video.mp4", *dedupe.Deduplication.SurvivorPath)
		claims, err := f.repo.SourceFileHistory.ClaimHistory(ctx, dedupe.Deduplication.ContentClaimUUID, "", 100)
		require.NoError(t, err)
		require.Len(t, claims, 1)
		rest, err := f.repo.CatalogFileHistoryImport.Records(ctx, f.manifest.UUID, page[0].Ordinal, 100)
		require.NoError(t, err)
		require.Len(t, rest, 55)
		for _, receipt := range rest {
			detail, err := f.repo.CatalogFileHistoryImport.Record(ctx, f.manifest.UUID, receipt.Ordinal)
			require.NoError(t, err)
			require.NotEmpty(t, detail.SourceValues)
			event, err := f.repo.SourceFileHistory.Find(ctx, *receipt.HistoryUUID)
			require.NoError(t, err)
			switch event.ReferenceValue {
			case "state":
				require.Equal(t, "deduplicated", event.StateChange.NewState)
			case "edit-000":
				require.Equal(t, models.SourceFileEdit{Field: "actors", TargetField: "performers", ValueType: "names", Mode: "set", Value: json.RawMessage(`["Ambiguous name"]`)}, event.Edits[0])
				require.Equal(t, "video.mp4", event.Locations[0].RelativePath)
			case "edit-001":
				require.Equal(t, "old.mp4", event.Locations[0].RelativePath, "a later duplicate-path title cannot replace the original path's choice")
			case "edit-002":
				require.Equal(t, "metadata_fields_require_review", receipt.Reason)
				require.Equal(t, "extension", event.Edits[1].ValueType)
				require.Equal(t, `{"source_id":9007199254740993123456789}`, string(event.Edits[1].Value))
				require.Equal(t, "inherit", event.Edits[3].Mode)
				require.Equal(t, "null", string(event.Edits[3].Value))
			}
		}
		obsPage, err := f.repo.SourceFileHistory.ObservationHistory(ctx, *dedupe.Locations[1].ObservationUUID, "", 1)
		require.NoError(t, err)
		require.Len(t, obsPage, 1)
		next, err := f.repo.SourceFileHistory.ObservationHistory(ctx, *dedupe.Locations[1].ObservationUUID, obsPage[0].UUID, 100)
		require.NoError(t, err)
		require.Len(t, next, 53)
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	check := openRawDB(t, output)
	defer check.Close()
	for _, table := range fileHistoryTables {
		require.Zero(t, queryUint(t, check, "SELECT count(*) FROM "+table), table)
	}
}

func TestCatalogFileHistoryRetainsMissingMembersAndUnsupportedValuesForReview(t *testing.T) {
	f := fileHistoryImportFixture(t, 0, func(rows []map[string]any) {
		for _, row := range rows {
			v := row["values"].(map[string]any)
			switch row["table"] {
			case "dedupe_events":
				v["paths_json"] = `["never-invented.mp4","video.mp4"]`
			case "file_events":
				v["observed_at"] = "invalid historical clock"
			case "metadata_edits":
				switch v["edit_id"] {
				case "edit-000":
					v["fields_json"] = `{"date":"not-a-date","actors":[],"title":false}`
				case "edit-001":
					v["relpath"] = "absent.mp4"
				}
			}
		}
	})
	result := advanceFileHistory(t, f, 0)
	require.EqualValues(t, 5, result.ReviewRecords)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.CatalogFileHistoryImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		require.Equal(t, "deduplication_member_requires_review", rows[0].Reason)
		event, err := f.repo.SourceFileHistory.Find(ctx, *rows[0].HistoryUUID)
		require.NoError(t, err)
		require.Nil(t, event.Locations[0].ObservationUUID)
		require.NotNil(t, event.Locations[1].ObservationUUID)
		require.Equal(t, "invalid_history_values", rows[1].Reason)
		require.Nil(t, rows[1].HistoryUUID)
		require.Equal(t, "file_observation_requires_review", rows[3].Reason)
		require.Nil(t, rows[3].HistoryUUID)
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestCatalogFileHistoryReceiptFailureRollsBackEvenWhenCallerSwallowsError(t *testing.T) {
	f := fileHistoryImportFixture(t, 0, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	attachmentSQL(t, f.db, `CREATE TRIGGER reject_history_receipt BEFORE INSERT ON catalog_file_history_records BEGIN SELECT RAISE(ABORT,'receipt failure'); END`)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogFileHistoryImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		require.ErrorContains(t, err, "receipt failure")
		return nil
	})
	require.ErrorContains(t, err, "did not finish atomically")
	for _, table := range fileHistoryTables {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
}

var fileHistoryTables = []string{"catalog_file_history_records", "catalog_file_history_imports", "source_file_history_locations", "source_file_history_edits", "source_file_history_states", "source_file_history_deduplications", "source_file_history"}

func removeSourceFileHistorySchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeMetadataFileReviewSchema(t, raw)
	for _, table := range fileHistoryTables {
		_, err := raw.Exec("DROP TABLE " + table)
		require.NoError(t, err)
	}
	_, err := raw.Exec("DELETE FROM native_migration_history WHERE version=1000066")
	require.NoError(t, err)
}

func TestCatalogFileHistoryMigrationPreservesPriorMediaAndRejectsCollisions(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			f := fileHistoryImportFixture(t, 0, nil)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			before := albumJobRows(t, raw, "source_file_observations")
			removeSourceFileHistorySchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000065,dirty=0")
			require.NoError(t, err)
			var needed *sqlite.MigrationNeededError
			require.ErrorAs(t, f.db.Open(f.db.DatabasePath()), &needed)
			if collision {
				_, err := raw.Exec("CREATE TABLE source_file_history(private_data TEXT); INSERT INTO source_file_history VALUES('preserve')")
				require.NoError(t, err)
				bytes, err := os.ReadFile(f.db.DatabasePath())
				require.NoError(t, err)
				require.Error(t, f.db.RunAllMigrations())
				after, err := os.ReadFile(f.db.DatabasePath())
				require.NoError(t, err)
				require.Equal(t, bytes, after)
				return
			}
			require.NoError(t, f.db.RunAllMigrations())
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
			require.Equal(t, before, albumJobRows(t, raw, "source_file_observations"))
			require.Equal(t, "review", advanceFileHistory(t, f, 0).State)
		})
	}
}
