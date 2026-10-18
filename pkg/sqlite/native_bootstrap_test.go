package sqlite

import (
	"context"
	"errors"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stretchr/testify/require"
)

func bootstrapTestMigrator(t *testing.T, path string) *Migrator {
	t.Helper()
	db := NewDatabase()
	db.dbPath = path
	conn, err := db.open(true, true)
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	m := &Migrator{db: db, conn: conn}
	t.Cleanup(m.Close)
	return m
}

func bootstrapAuthorizer(t *testing.T, conn *sqlx.DB, callback func(int, string, string, string) int) {
	t.Helper()
	raw, err := conn.Conn(t.Context())
	require.NoError(t, err)
	require.NoError(t, raw.Raw(func(driverConn any) error {
		driverConn.(*CustomSQLiteConn).RegisterAuthorizer(callback)
		return nil
	}))
	require.NoError(t, raw.Close())
}

func TestNativeBootstrapFreshAndReopen(t *testing.T) {
	config.InitializeEmpty()
	// Any attempted historical reader is an error, even with no rows to read.
	old := preMigrations[1]
	preMigrations[1] = append([]customMigrationFunc{func(context.Context, *sqlx.DB) error {
		return errors.New("historical initialization must not run")
	}}, old...)
	oldFilter := preMigrations[1000001]
	preMigrations[1000001] = preMigrations[1]
	t.Cleanup(func() { preMigrations[1], preMigrations[1000001] = old, oldFilter })
	for _, kind := range []string{"missing", "zero-length", "empty-sqlite"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "native #1.sqlite")
			switch kind {
			case "zero-length":
				require.NoError(t, os.WriteFile(path, nil, 0600))
			case "empty-sqlite":
				m := bootstrapTestMigrator(t, path)
				_, err := m.conn.Exec("PRAGMA journal_mode=WAL")
				require.NoError(t, err)
				m.Close()
			}
			db := NewDatabase()
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			require.NoError(t, db.Open(path))
			require.Equal(t, GetRequiredSchemaVersion(), db.Version())
			require.NoError(t, db.Close())
			for range 2 {
				require.NoError(t, db.Open(path))
				require.NoError(t, db.Close())
			}
			m, err := NewMigrator(db)
			require.NoError(t, err)
			defer m.Close()
			created, err := m.bootstrapNative(t.Context())
			require.NoError(t, err)
			require.False(t, created)
		})
	}
}

func TestNativeBootstrapFailureRollsBackAndRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "failed.sqlite")
	m := bootstrapTestMigrator(t, path)
	interrupted := false
	bootstrapAuthorizer(t, m.conn, func(operation int, _, _, _ string) int {
		if operation == sqlite3.SQLITE_CREATE_TRIGGER {
			// Tables and initial data have already been written at this point.
			interrupted = true
			return sqlite3.SQLITE_DENY
		}
		return sqlite3.SQLITE_OK
	})
	created, err := m.bootstrapNative(t.Context())
	require.ErrorContains(t, err, "initializing native schema")
	require.False(t, created)
	require.True(t, interrupted)
	bootstrapAuthorizer(t, m.conn, nil)
	fresh, err := isPristineDatabase(t.Context(), m.conn)
	require.NoError(t, err)
	require.True(t, fresh, "failed initialization must leave no ledger, objects or initial rows")
	m.Close()
	db := NewDatabase()
	require.NoError(t, db.Open(path))
	require.NoError(t, db.Close())
}

func TestNativeBootstrapCommitFailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commit-failed.sqlite")
	m := bootstrapTestMigrator(t, path)
	bootstrapAuthorizer(t, m.conn, func(operation int, action, _, _ string) int {
		if operation == sqlite3.SQLITE_TRANSACTION && action == "COMMIT" {
			return sqlite3.SQLITE_DENY
		}
		return sqlite3.SQLITE_OK
	})
	created, err := m.bootstrapNative(t.Context())
	require.ErrorContains(t, err, "committing native initialization")
	require.False(t, created)
	bootstrapAuthorizer(t, m.conn, nil)
	fresh, err := isPristineDatabase(t.Context(), m.conn)
	require.NoError(t, err)
	require.True(t, fresh)
	created, err = m.bootstrapNative(t.Context())
	require.NoError(t, err)
	require.True(t, created)
}

func TestNativeBootstrapRechecksUnderWriterLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changed.sqlite")
	m := bootstrapTestMigrator(t, path)
	other := bootstrapTestMigrator(t, path)
	var changeErr error
	changed := false
	bootstrapAuthorizer(t, m.conn, func(operation int, action, _, _ string) int {
		if operation == sqlite3.SQLITE_TRANSACTION && action == "BEGIN" && !changed {
			// Another writer commits after the initial check but before BEGIN.
			changed = true
			_, changeErr = other.conn.Exec("CREATE TABLE other_data(value TEXT); INSERT INTO other_data VALUES ('retained')")
		}
		return sqlite3.SQLITE_OK
	})
	created, err := m.bootstrapNative(t.Context())
	require.ErrorContains(t, err, "database changed during native initialization")
	require.NoError(t, changeErr)
	require.True(t, changed)
	require.False(t, created)
	bootstrapAuthorizer(t, m.conn, nil)
	var value string
	require.NoError(t, m.conn.Get(&value, "SELECT value FROM other_data"))
	require.Equal(t, "retained", value)
	var count int
	require.NoError(t, m.conn.Get(&count, "SELECT count(*) FROM sqlite_schema"))
	require.Equal(t, 1, count, "no baseline or migration-driver ledger may be added")
}

func TestNativeBootstrapProcessInterruption(t *testing.T) {
	const environment = "STASH_TEST_BOOTSTRAP_INTERRUPTION"
	if path := os.Getenv(environment); path != "" {
		m := bootstrapTestMigrator(t, path)
		bootstrapAuthorizer(t, m.conn, func(operation int, _, _, _ string) int {
			if operation == sqlite3.SQLITE_CREATE_TRIGGER {
				// Abrupt exit bypasses deferred Rollback/Close during the real
				// bootstrap transaction, after its DDL and initial data writes.
				os.Exit(23)
			}
			return sqlite3.SQLITE_OK
		})
		_, _ = m.bootstrapNative(t.Context())
		t.Fatal("bootstrap did not reach interruption point")
	}
	path := filepath.Join(t.TempDir(), "interrupted.sqlite")
	command := osexec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestNativeBootstrapProcessInterruption$")
	command.Env = append(os.Environ(), environment+"="+path)
	output, err := command.CombinedOutput()
	var exited *osexec.ExitError
	require.ErrorAs(t, err, &exited, "%s", output)
	require.Equal(t, 23, exited.ExitCode(), "%s", output)
	m := bootstrapTestMigrator(t, path)
	fresh, err := isPristineDatabase(t.Context(), m.conn)
	require.NoError(t, err)
	require.True(t, fresh)
	m.Close()
	db := NewDatabase()
	require.NoError(t, db.Open(path))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	require.NoError(t, db.Close())
}

func TestNativeBootstrapCommittedBaselineResumesNativeMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.sqlite")
	m := bootstrapTestMigrator(t, path)
	created, err := m.bootstrapNative(t.Context())
	require.NoError(t, err)
	require.True(t, created)
	var version uint
	require.NoError(t, m.conn.Get(&version, "SELECT version FROM schema_migrations WHERE dirty=0"))
	require.Equal(t, nativeBootstrapVersion, version)
	require.Less(t, version, GetRequiredSchemaVersion(), "exercise real migrations after the pinned baseline")
	// A record added before the later migration must survive a reopen/retry.
	_, err = m.conn.Exec("INSERT INTO scenes(title,created_at,updated_at) VALUES ('retained',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)")
	require.NoError(t, err)
	m.Close()
	db := NewDatabase()
	var needed *MigrationNeededError
	require.ErrorAs(t, db.Open(path), &needed)
	require.Equal(t, nativeBootstrapVersion, needed.CurrentSchemaVersion)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.Open(path))
	require.NoError(t, db.Close())
	require.NoError(t, db.RunAllMigrations())
	m = bootstrapTestMigrator(t, path)
	var count int
	require.NoError(t, m.conn.Get(&count, "SELECT count(*) FROM scenes WHERE title='retained'"))
	require.Equal(t, 1, count)
	require.NoError(t, m.conn.Get(&count, "SELECT count(*) FROM native_migration_history WHERE version=1000122"))
	require.Equal(t, 1, count)
}

func TestNativeBootstrapRejectsNonfreshInputsWithoutWriting(t *testing.T) {
	for _, tc := range []struct{ name, setup string }{
		{"foreign table", "CREATE TABLE foreign_data(value TEXT); INSERT INTO foreign_data VALUES ('retained')"},
		{"view only", "CREATE VIEW foreign_view AS SELECT 1"},
		{"previously used", "CREATE TABLE old_data(value TEXT); DROP TABLE old_data"},
		{"application id", "PRAGMA application_id=123"},
		{"user version", "PRAGMA user_version=123"},
		{"partial with empty ledger", "CREATE TABLE schema_migrations(version, dirty); CREATE TABLE retained(value)"},
		{"view with empty ledger", "CREATE TABLE schema_migrations(version, dirty); CREATE VIEW retained AS SELECT 1"},
		{"zero version", "CREATE TABLE schema_migrations(version, dirty); INSERT INTO schema_migrations VALUES (0,0)"},
		{"dirty first migration", "CREATE TABLE schema_migrations(version, dirty); INSERT INTO schema_migrations VALUES (1,1)"},
		{"newer without lineage", "CREATE TABLE schema_migrations(version, dirty); INSERT INTO schema_migrations VALUES (2000000,0)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "existing.sqlite")
			m := bootstrapTestMigrator(t, path)
			_, err := m.conn.Exec(tc.setup)
			require.NoError(t, err)
			m.Close()
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			db := NewDatabase()
			require.Error(t, db.Open(path))
			_, err = NewMigrator(db)
			require.Error(t, err)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestNativeBootstrapLegacyEmptyLedgerKeepsHistoricalRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-driver.sqlite")
	m := bootstrapTestMigrator(t, path)
	_, err := m.conn.Exec("CREATE TABLE schema_migrations (version uint64,dirty bool); CREATE UNIQUE INDEX version_unique ON schema_migrations(version)")
	require.NoError(t, err)
	created, err := m.bootstrapNative(t.Context())
	require.NoError(t, err)
	require.False(t, created)
	m.Close()
	// Observe the real historical first step, then let it complete normally.
	old := preMigrations[1]
	called := false
	preMigrations[1] = append([]customMigrationFunc{func(context.Context, *sqlx.DB) error {
		called = true
		return nil
	}}, old...)
	t.Cleanup(func() { preMigrations[1] = old })
	db := NewDatabase()
	require.NoError(t, db.Open(path))
	require.True(t, called)
	require.NoError(t, db.Close())
}
