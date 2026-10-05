package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/sqlite"

	_ "github.com/stashapp/stash/pkg/sqlite/migrations"
)

func TestLegacyForkSchemaVersionIsAdopted(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })

	db := sqlite.NewDatabase()
	dbPath := filepath.Join(t.TempDir(), "stash-go.sqlite")

	// Private schemas 998/999 were based on upstream 85. Build that actual
	// historical schema, rather than relabelling the latest database: doing
	// the latter pre-applies every new upstream migration under test.
	initial, err := os.ReadFile("migrations/1_initial.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	raw := openRawDB(t, dbPath)
	if _, err := raw.Exec(string(initial)); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, dirty BOOLEAN NOT NULL); INSERT INTO schema_migrations VALUES (1, false)"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	var migrationErr *sqlite.MigrationNeededError
	if err := db.Open(dbPath); !errors.As(err, &migrationErr) {
		t.Fatalf("Open initial schema = %v, want MigrationNeededError", err)
	}
	migrator, err := sqlite.NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(migrator.Close)
	const legacyBase = uint(85)
	for current := migrator.CurrentSchemaVersion(); current < legacyBase; current = migrator.CurrentSchemaVersion() {
		if err := migrator.RunMigration(context.Background(), migrator.GetNextMigrationVersion(current)); err != nil {
			t.Fatal(err)
		}
	}
	migrator.Close()

	raw = openRawDB(t, dbPath)
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
	err = adopted.Open(dbPath)
	if !errors.As(err, &migrationErr) {
		t.Fatalf("Open error = %v, want MigrationNeededError", err)
	}
	if got, want := adopted.Version(), legacyBase; got != want {
		t.Fatalf("adopted upstream schema version = %d, want %d", got, want)
	}
	if got, want := migrationErr.RequiredSchemaVersion, adopted.AppSchemaVersion(); got != want {
		t.Fatalf("required upstream schema version = %d, want %d", got, want)
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
	if rawColumnExists(t, raw, "scenes", "production_date") {
		t.Fatal("legacy schema must not have production_date before migration")
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
	if got, want := queryUint(t, raw, "SELECT MAX(version) FROM legacy_schema_history WHERE track = 'legacy-fork'"), sqlite.GetRequiredForkSchemaVersion(); got != want {
		t.Fatalf("stored adopted fork schema version after migration = %d, want %d", got, want)
	}
	for _, column := range []string{"production_date", "production_date_precision"} {
		if !rawColumnExists(t, raw, "scenes", column) {
			t.Fatalf("upstream migration did not create scenes.%s", column)
		}
	}
}

func TestNativePromotionImportsMissingLegacyStateOnce(t *testing.T) {
	config.InitializeEmpty()

	dbPath := filepath.Join(t.TempDir(), "stash-go.sqlite")
	buildLegacyDatabase(t, dbPath, 86, true)

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
	var needed *sqlite.MigrationNeededError
	if err := reopened.Open(dbPath); !errors.As(err, &needed) {
		t.Fatalf("Open legacy database: %v", err)
	}
	if err := reopened.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Open(dbPath); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("Close reconciled database: %v", err)
	}

	raw = openRawDB(t, dbPath)
	defer raw.Close()
	if rawTableExists(t, raw, "saved_filter_state") {
		t.Fatal("saved-filter sidecar remains after canonical conversion")
	}
	if got, want := queryUint(t, raw, "SELECT COUNT(*) FROM saved_filters WHERE filter_ast != ''"), uint(1); got != want {
		t.Fatalf("reconciled saved-filter count = %d, want %d", got, want)
	}
}

func TestPrivateForkVersionFourUpgradesToConsolidatedMigration(t *testing.T) {
	config.InitializeEmpty()

	dbPath := filepath.Join(t.TempDir(), "stash-go.sqlite")
	buildLegacyDatabase(t, dbPath, 86, true)

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
	if got, want := queryUint(t, raw, "SELECT MAX(version) FROM legacy_schema_history WHERE track = 'legacy-fork'"), sqlite.GetRequiredForkSchemaVersion(); got != want {
		t.Fatalf("consolidated fork version = %d, want %d", got, want)
	}
	if got, want := queryUint(t, raw, "SELECT COUNT(*) FROM legacy_schema_history WHERE track = 'legacy-fork'"), uint(9); got != want {
		t.Fatalf("consolidated migration count = %d, want %d", got, want)
	}
	if rawColumnExists(t, raw, "performer_aliases", "ignore_auto_tag") || rawTableExists(t, raw, "saved_filter_state") {
		t.Fatal("transitional fork state remains after canonical conversion")
	}
	if !rawColumnExists(t, raw, "saved_filters", "filter_ast") || rawColumnExists(t, raw, "saved_filters", "object_filter") {
		t.Fatal("saved filters must store only their canonical AST")
	}
}

func openRawDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()

	abs, err := filepath.Abs(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: "_fk=true"}
	db, err := sql.Open("sqlite3ex", uri.String())
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
