package migrations

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
	_ "github.com/stashapp/stash/pkg/sqlite"
)

func TestFork005MigratesCompatibilitySidecars(t *testing.T) {
	db := openCompatibilityTestDB(t)
	defer db.Close()

	ast := &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{
		Field: "rating100",
		Value: map[string]interface{}{"value": 80, "modifier": "GREATER_THAN"},
	}}}
	encodedAST, err := json.Marshal(ast)
	if err != nil {
		t.Fatalf("encoding AST: %v", err)
	}

	statements := []string{
		"CREATE TABLE performers (id INTEGER PRIMARY KEY, name TEXT)",
		"CREATE TABLE performer_aliases (performer_id INTEGER NOT NULL, alias TEXT NOT NULL, ignore_auto_tag BOOLEAN DEFAULT 1 NOT NULL)",
		"CREATE TABLE saved_filters (id INTEGER PRIMARY KEY, object_filter BLOB, filter_ast BLOB NOT NULL DEFAULT '')",
		"CREATE TABLE files (id INTEGER PRIMARY KEY, size INTEGER NOT NULL, mod_time DATETIME NOT NULL)",
		`CREATE TABLE video_files (
  file_id INTEGER PRIMARY KEY, video_stream_duration REAL, frame_count INTEGER,
  duration_mismatch BOOLEAN NOT NULL DEFAULT 0, bit_depth INTEGER, color_range TEXT,
  color_space TEXT, color_transfer TEXT, color_primaries TEXT)`,
		`CREATE TABLE image_files (
  file_id INTEGER PRIMARY KEY, bit_depth INTEGER, color_range TEXT,
  color_space TEXT, color_transfer TEXT, color_primaries TEXT)`,
		"INSERT INTO performers (id, name) VALUES (1, 'Primary Name')",
		"INSERT INTO performer_aliases VALUES (1, 'Ignored Alias', 1), (1, 'Enabled Alias', 0)",
		"INSERT INTO saved_filters (id, object_filter, filter_ast) VALUES (1, '', '" + string(encodedAST) + "')",
		"INSERT INTO files VALUES (10, 1000, '2026-01-02T03:04:05Z'), (11, 2000, '2026-02-03T04:05:06Z')",
		"INSERT INTO video_files VALUES (10, 12.5, 300, 1, 10, 'tv', 'bt2020', 'smpte2084', 'bt2020')",
		"INSERT INTO image_files VALUES (11, 16, 'pc', 'rgb', 'iec61966-2-1', 'bt709')",
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("executing %q: %v", statement, err)
		}
	}

	if err := fork005CompatibilitySidecars(context.Background(), db); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	if err := fork005CompatibilitySidecars(context.Background(), db); err != nil {
		t.Fatalf("rerunning migration: %v", err)
	}

	assertColumnMissing(t, db, "performer_aliases", "ignore_auto_tag")
	assertColumnMissing(t, db, "saved_filters", "filter_ast")
	for _, column := range []string{"video_stream_duration", "frame_count", "duration_mismatch", "bit_depth", "color_range", "color_space", "color_transfer", "color_primaries"} {
		assertColumnMissing(t, db, "video_files", column)
	}
	for _, column := range []string{"bit_depth", "color_range", "color_space", "color_transfer", "color_primaries"} {
		assertColumnMissing(t, db, "image_files", column)
	}

	assertInt(t, db, "SELECT COUNT(*) FROM fork_performer_autotag_ignored_names", 1)
	assertString(t, db, "SELECT name FROM fork_performer_autotag_ignored_names", "Ignored Alias")
	assertString(t, db, "SELECT filter_ast FROM fork_saved_filter_state WHERE saved_filter_id = 1", string(encodedAST))
	assertString(t, db, "SELECT object_filter FROM saved_filters WHERE id = 1", `{"rating100":{"modifier":"GREATER_THAN","value":80}}`)
	assertInt(t, db, "SELECT frame_count FROM fork_video_file_metadata WHERE file_id = 10", 300)
	assertInt(t, db, "SELECT bit_depth FROM fork_image_file_metadata WHERE file_id = 11", 16)
}

func TestForkReconcilersImportAndInvalidateUpstreamChanges(t *testing.T) {
	db := openCompatibilityTestDB(t)
	defer db.Close()
	complexAST := &models.FilterAST{Root: &models.FilterASTNode{Group: &models.FilterASTGroup{
		Operator: models.FilterGroupOperatorOr,
		Children: []*models.FilterASTNode{
			{Condition: &models.FilterASTCondition{Field: "rating100", Value: map[string]interface{}{"value": 80, "modifier": "GREATER_THAN"}}},
			{Condition: &models.FilterASTCondition{Field: "resolution", Value: map[string]interface{}{"value": "FULL_HD", "modifier": "EQUALS"}}},
		},
	}}}
	encodedComplexAST, err := json.Marshal(complexAST)
	if err != nil {
		t.Fatalf("encoding complex AST: %v", err)
	}

	statements := []string{
		"CREATE TABLE performers (id INTEGER PRIMARY KEY, name TEXT)",
		"CREATE TABLE performer_aliases (performer_id INTEGER NOT NULL, alias TEXT NOT NULL)",
		"CREATE TABLE saved_filters (id INTEGER PRIMARY KEY, object_filter BLOB)",
		"CREATE TABLE files (id INTEGER PRIMARY KEY, size INTEGER NOT NULL, mod_time DATETIME NOT NULL)",
		"CREATE TABLE video_files (file_id INTEGER PRIMARY KEY)",
		"CREATE TABLE image_files (file_id INTEGER PRIMARY KEY)",
		createPerformerAutoTagIgnoredNamesTable,
		createSavedFilterStateTable,
		createVideoFileMetadataTable,
		createImageFileMetadataTable,
		"INSERT INTO performers VALUES (1, 'Primary Name')",
		"INSERT INTO performer_aliases VALUES (1, 'Old Alias')",
		"INSERT INTO fork_performer_autotag_ignored_names VALUES (1, 'Old Alias')",
		`INSERT INTO saved_filters VALUES (1, '{"rating100":{"value":80,"modifier":"GREATER_THAN"}}')`,
		`INSERT INTO fork_saved_filter_state VALUES (1, '` + string(encodedComplexAST) + `', '{"rating100":{"value":80,"modifier":"GREATER_THAN"}}', NULL)`,
		"INSERT INTO files VALUES (10, 1000, '2026-01-02T03:04:05Z')",
		"INSERT INTO video_files VALUES (10)",
		"INSERT INTO fork_video_file_metadata (file_id, source_size, source_mod_time, frame_count) VALUES (10, 1000, '2026-01-02T03:04:05Z', 300)",
		"DELETE FROM performer_aliases WHERE performer_id = 1",
		"UPDATE files SET size = 1001 WHERE id = 10",
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("executing %q: %v", statement, err)
		}
	}

	ctx := context.Background()
	if err := reconcilePerformerAutoTagNames(ctx, db); err != nil {
		t.Fatalf("reconciling performer names: %v", err)
	}
	if err := reconcileSavedFilters(ctx, db); err != nil {
		t.Fatalf("reconciling saved filters: %v", err)
	}
	assertString(t, db, "SELECT filter_ast FROM fork_saved_filter_state WHERE saved_filter_id = 1", string(encodedComplexAST))
	if _, err := db.Exec(`UPDATE saved_filters SET object_filter = '{"resolution":{"value":"FULL_HD","modifier":"EQUALS"}}' WHERE id = 1`); err != nil {
		t.Fatalf("updating upstream saved filter: %v", err)
	}
	if err := reconcileSavedFilters(ctx, db); err != nil {
		t.Fatalf("reconciling changed saved filter: %v", err)
	}
	if err := reconcileFileMetadata(ctx, db); err != nil {
		t.Fatalf("reconciling file metadata: %v", err)
	}

	assertInt(t, db, "SELECT COUNT(*) FROM fork_performer_autotag_ignored_names", 0)
	assertInt(t, db, "SELECT COUNT(*) FROM fork_video_file_metadata", 0)
	assertString(t, db, "SELECT legacy_object_filter FROM fork_saved_filter_state WHERE saved_filter_id = 1", `{"rating100":{"value":80,"modifier":"GREATER_THAN"}}`)
	assertString(t, db, "SELECT pending_legacy_object_filter FROM fork_saved_filter_state WHERE saved_filter_id = 1", `{"resolution":{"value":"FULL_HD","modifier":"EQUALS"}}`)
	var encodedAST string
	if err := db.Get(&encodedAST, "SELECT filter_ast FROM fork_saved_filter_state WHERE saved_filter_id = 1"); err != nil {
		t.Fatalf("reading reconciled AST: %v", err)
	}
	if encodedAST == "" {
		t.Fatal("reconciled AST is empty")
	}
	if encodedAST != string(encodedComplexAST) {
		t.Fatal("changed upstream filter replaced the complex v3 AST")
	}
}

func openCompatibilityTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite3ex", ":memory:")
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	db.SetMaxOpenConns(1)
	return db
}

func assertColumnMissing(t *testing.T, db *sqlx.DB, tableName string, columnName string) {
	t.Helper()
	exists, err := forkColumnExists(context.Background(), db, tableName, columnName)
	if err != nil {
		t.Fatalf("checking %s.%s: %v", tableName, columnName, err)
	}
	if exists {
		t.Fatalf("legacy column %s.%s still exists", tableName, columnName)
	}
}

func assertInt(t *testing.T, db *sqlx.DB, query string, want int) {
	t.Helper()
	var got int
	if err := db.Get(&got, query); err != nil {
		t.Fatalf("running %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q = %d, want %d", query, got, want)
	}
}

func assertString(t *testing.T, db *sqlx.DB, query string, want string) {
	t.Helper()
	var got string
	if err := db.Get(&got, query); err != nil {
		t.Fatalf("running %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q = %q, want %q", query, got, want)
	}
}
