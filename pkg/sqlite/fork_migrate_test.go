package sqlite_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/sqlite"

	_ "github.com/stashapp/stash/pkg/sqlite/migrations"
)

func TestForkMigrationsRunOutsideUpstreamSchemaVersion(t *testing.T) {
	config.InitializeEmpty()

	db := sqlite.NewDatabase()
	dbPath := filepath.Join(t.TempDir(), "stash-go.sqlite")

	if err := db.Open(dbPath); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got, want := db.Version(), db.AppSchemaVersion(); got != want {
		t.Fatalf("upstream schema version = %d, want %d", got, want)
	}
	if got, want := db.ForkSchemaVersion(), db.RequiredForkSchemaVersion(); got != want {
		t.Fatalf("fork schema version = %d, want %d", got, want)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw := openRawDB(t, dbPath)
	defer raw.Close()

	if got, want := queryUint(t, raw, "SELECT version FROM schema_migrations LIMIT 1"), db.AppSchemaVersion(); got != want {
		t.Fatalf("stored upstream schema version = %d, want %d", got, want)
	}
	if got, want := queryUint(t, raw, "SELECT MAX(version) FROM fork_schema_migrations"), db.RequiredForkSchemaVersion(); got != want {
		t.Fatalf("stored fork schema version = %d, want %d", got, want)
	}
	if rawColumnExists(t, raw, "performer_aliases", "ignore_auto_tag") {
		t.Fatal("performer_aliases.ignore_auto_tag column was not removed")
	}
	if !rawTableExists(t, raw, "fork_performer_autotag_ignored_names") {
		t.Fatal("fork_performer_autotag_ignored_names table was not created")
	}
	for _, tableName := range []string{"fork_saved_filter_state", "fork_video_file_metadata", "fork_image_file_metadata"} {
		if !rawTableExists(t, raw, tableName) {
			t.Fatalf("%s table was not created", tableName)
		}
	}
	if rawColumnExists(t, raw, "saved_filters", "filter_ast") {
		t.Fatal("saved_filters.filter_ast column was not removed")
	}
	for _, column := range []string{"video_stream_duration", "frame_count", "duration_mismatch", "bit_depth", "color_range", "color_space", "color_transfer", "color_primaries"} {
		if rawColumnExists(t, raw, "video_files", column) {
			t.Fatalf("video_files.%s column was not removed", column)
		}
	}
	for _, column := range []string{"bit_depth", "color_range", "color_space", "color_transfer", "color_primaries"} {
		if rawColumnExists(t, raw, "image_files", column) {
			t.Fatalf("image_files.%s column was not removed", column)
		}
	}
	if got, want := queryUint(t, raw, "SELECT COUNT(*) FROM fork_schema_migrations"), uint(1); got != want {
		t.Fatalf("fork migration count = %d, want %d", got, want)
	}
}

func TestLegacyForkSchemaVersionIsAdopted(t *testing.T) {
	config.InitializeEmpty()

	db := sqlite.NewDatabase()
	dbPath := filepath.Join(t.TempDir(), "stash-go.sqlite")

	if err := db.Open(dbPath); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw := openRawDB(t, dbPath)
	if _, err := raw.Exec("DROP TABLE fork_schema_migrations"); err != nil {
		t.Fatalf("dropping fork schema table: %v", err)
	}
	if _, err := raw.Exec("DELETE FROM schema_migrations"); err != nil {
		t.Fatalf("clearing schema_migrations: %v", err)
	}
	if _, err := raw.Exec("INSERT INTO schema_migrations (version, dirty) VALUES (?, ?)", 998, false); err != nil {
		t.Fatalf("setting legacy schema_migrations: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("closing raw db: %v", err)
	}

	adopted := sqlite.NewDatabase()
	err := adopted.Open(dbPath)
	var migrationErr *sqlite.MigrationNeededError
	if !errors.As(err, &migrationErr) {
		t.Fatalf("Open error = %v, want MigrationNeededError", err)
	}
	if got, want := adopted.Version(), adopted.AppSchemaVersion(); got != want {
		t.Fatalf("adopted upstream schema version = %d, want %d", got, want)
	}
	if got, want := adopted.ForkSchemaVersion(), uint(0); got != want {
		t.Fatalf("adopted fork schema version = %d, want %d", got, want)
	}
	if got, want := migrationErr.CurrentForkSchemaVersion, uint(0); got != want {
		t.Fatalf("migration error current fork schema = %d, want %d", got, want)
	}
	if got, want := migrationErr.RequiredForkSchemaVersion, adopted.RequiredForkSchemaVersion(); got != want {
		t.Fatalf("migration error required fork schema = %d, want %d", got, want)
	}

	raw = openRawDB(t, dbPath)
	if got, want := queryUint(t, raw, "SELECT version FROM schema_migrations LIMIT 1"), uint(998); got != want {
		t.Fatalf("stored legacy upstream schema version before migration = %d, want %d", got, want)
	}
	if rawTableExists(t, raw, "fork_schema_migrations") {
		t.Fatal("fork_schema_migrations should not be written during Open")
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("closing raw db: %v", err)
	}

	if err := adopted.RunAllMigrations(); err != nil {
		t.Fatalf("RunAllMigrations: %v", err)
	}

	raw = openRawDB(t, dbPath)
	defer raw.Close()
	if got, want := queryUint(t, raw, "SELECT version FROM schema_migrations LIMIT 1"), adopted.AppSchemaVersion(); got != want {
		t.Fatalf("stored adopted upstream schema version after migration = %d, want %d", got, want)
	}
	if got, want := queryUint(t, raw, "SELECT MAX(version) FROM fork_schema_migrations"), adopted.RequiredForkSchemaVersion(); got != want {
		t.Fatalf("stored adopted fork schema version after migration = %d, want %d", got, want)
	}
}

func TestForkReconcilersRunWhenDatabaseOpens(t *testing.T) {
	config.InitializeEmpty()

	dbPath := filepath.Join(t.TempDir(), "stash-go.sqlite")
	db := sqlite.NewDatabase()
	if err := db.Open(dbPath); err != nil {
		t.Fatalf("Open initial database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close initial database: %v", err)
	}

	raw := openRawDB(t, dbPath)
	if _, err := raw.Exec("DROP TABLE fork_saved_filter_state"); err != nil {
		t.Fatalf("dropping saved-filter sidecar: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO saved_filters (name, mode, object_filter) VALUES (?, ?, ?)`,
		"upstream filter", "SCENES", `{"rating100":{"value":80,"modifier":"GREATER_THAN"}}`); err != nil {
		t.Fatalf("inserting upstream saved filter: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("closing upstream database: %v", err)
	}

	reopened := sqlite.NewDatabase()
	if err := reopened.Open(dbPath); err != nil {
		t.Fatalf("Open reconciled database: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("Close reconciled database: %v", err)
	}

	raw = openRawDB(t, dbPath)
	defer raw.Close()
	if !rawTableExists(t, raw, "fork_saved_filter_state") {
		t.Fatal("saved-filter sidecar was not recreated")
	}
	if got, want := queryUint(t, raw, "SELECT COUNT(*) FROM fork_saved_filter_state"), uint(1); got != want {
		t.Fatalf("reconciled saved-filter count = %d, want %d", got, want)
	}
}

func TestPrivateForkVersionFourUpgradesToConsolidatedMigration(t *testing.T) {
	config.InitializeEmpty()

	dbPath := filepath.Join(t.TempDir(), "stash-go.sqlite")
	db := sqlite.NewDatabase()
	if err := db.Open(dbPath); err != nil {
		t.Fatalf("Open initial database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close initial database: %v", err)
	}

	raw := openRawDB(t, dbPath)
	statements := []string{
		"DROP TABLE fork_performer_autotag_ignored_names",
		"DROP TABLE fork_saved_filter_state",
		"DROP TABLE fork_video_file_metadata",
		"DROP TABLE fork_image_file_metadata",
		"ALTER TABLE performer_aliases ADD COLUMN ignore_auto_tag BOOLEAN DEFAULT 1 NOT NULL",
		"ALTER TABLE saved_filters ADD COLUMN filter_ast BLOB NOT NULL DEFAULT ''",
		"ALTER TABLE video_files ADD COLUMN video_stream_duration REAL",
		"ALTER TABLE video_files ADD COLUMN frame_count INTEGER",
		"ALTER TABLE video_files ADD COLUMN duration_mismatch BOOLEAN NOT NULL DEFAULT 0",
		"ALTER TABLE video_files ADD COLUMN bit_depth INTEGER",
		"ALTER TABLE video_files ADD COLUMN color_range VARCHAR(255)",
		"ALTER TABLE video_files ADD COLUMN color_space VARCHAR(255)",
		"ALTER TABLE video_files ADD COLUMN color_transfer VARCHAR(255)",
		"ALTER TABLE video_files ADD COLUMN color_primaries VARCHAR(255)",
		"ALTER TABLE image_files ADD COLUMN bit_depth INTEGER",
		"ALTER TABLE image_files ADD COLUMN color_range VARCHAR(255)",
		"ALTER TABLE image_files ADD COLUMN color_space VARCHAR(255)",
		"ALTER TABLE image_files ADD COLUMN color_transfer VARCHAR(255)",
		"ALTER TABLE image_files ADD COLUMN color_primaries VARCHAR(255)",
		"DELETE FROM fork_schema_migrations",
		"INSERT INTO fork_schema_migrations (version, name) VALUES (1, 'legacy 1'), (2, 'legacy 2'), (3, 'legacy 3'), (4, 'legacy 4')",
	}
	for _, statement := range statements {
		if _, err := raw.Exec(statement); err != nil {
			raw.Close()
			t.Fatalf("executing %q: %v", statement, err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("closing version-four database: %v", err)
	}

	upgrade := sqlite.NewDatabase()
	err := upgrade.Open(dbPath)
	var migrationErr *sqlite.MigrationNeededError
	if !errors.As(err, &migrationErr) {
		t.Fatalf("Open error = %v, want MigrationNeededError", err)
	}
	if got, want := migrationErr.CurrentForkSchemaVersion, uint(4); got != want {
		t.Fatalf("current fork version = %d, want %d", got, want)
	}
	if err := upgrade.RunAllMigrations(); err != nil {
		t.Fatalf("RunAllMigrations: %v", err)
	}

	raw = openRawDB(t, dbPath)
	defer raw.Close()
	if got, want := queryUint(t, raw, "SELECT MAX(version) FROM fork_schema_migrations"), uint(5); got != want {
		t.Fatalf("consolidated fork version = %d, want %d", got, want)
	}
	if got, want := queryUint(t, raw, "SELECT COUNT(*) FROM fork_schema_migrations"), uint(1); got != want {
		t.Fatalf("consolidated migration count = %d, want %d", got, want)
	}
	if rawColumnExists(t, raw, "performer_aliases", "ignore_auto_tag") || rawColumnExists(t, raw, "saved_filters", "filter_ast") {
		t.Fatal("private fork columns remain after consolidated migration")
	}
}

func openRawDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3ex", "file:"+dbPath+"?_fk=true")
	if err != nil {
		t.Fatalf("opening raw sqlite db: %v", err)
	}
	return db
}

func queryUint(t *testing.T, db *sql.DB, query string) uint {
	t.Helper()

	var ret uint
	if err := db.QueryRow(query).Scan(&ret); err != nil {
		t.Fatalf("querying %q: %v", query, err)
	}
	return ret
}

func rawColumnExists(t *testing.T, db *sql.DB, tableName string, columnName string) bool {
	t.Helper()

	rows, err := db.Query("PRAGMA table_info(`" + tableName + "`)")
	if err != nil {
		t.Fatalf("reading columns for %s: %v", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			t.Fatalf("scanning columns for %s: %v", tableName, err)
		}
		if name == columnName {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading columns for %s: %v", tableName, err)
	}

	return false
}

func rawTableExists(t *testing.T, db *sql.DB, tableName string) bool {
	t.Helper()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", tableName).Scan(&count); err != nil {
		t.Fatalf("checking table %s: %v", tableName, err)
	}

	return count > 0
}
