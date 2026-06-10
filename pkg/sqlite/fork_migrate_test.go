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
	if !rawColumnExists(t, raw, "performer_aliases", "ignore_auto_tag") {
		t.Fatal("performer_aliases.ignore_auto_tag column was not created")
	}
	if !rawColumnExists(t, raw, "saved_filters", "filter_ast") {
		t.Fatal("saved_filters.filter_ast column was not created")
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
	if got, want := adopted.ForkSchemaVersion(), uint(1); got != want {
		t.Fatalf("adopted fork schema version = %d, want %d", got, want)
	}
	if got, want := migrationErr.CurrentForkSchemaVersion, uint(1); got != want {
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
