package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type historicalFixtureDatabase interface {
	Query(string, ...any) (*sql.Rows, error)
	Exec(string, ...any) (sql.Result, error)
}

func copyHistoricalFixtureTable(t *testing.T, db historicalFixtureDatabase, table string) {
	t.Helper()
	rows, err := db.Query("SELECT * FROM " + table + " LIMIT 0")
	require.NoError(t, err)
	defer rows.Close()
	columns, err := rows.Columns()
	require.NoError(t, err)
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	for i := range columns {
		columns[i] = `"` + strings.ReplaceAll(columns[i], `"`, `""`) + `"`
	}
	fields := strings.Join(columns, ",")
	_, err = db.Exec("INSERT INTO " + table + "(" + fields + ") SELECT " + fields + " FROM source_fixture." + table)
	require.NoError(t, err, table)
}

func removeCaptureRecordingTimeSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_captures WHERE captured_at IS NULL"))
	old, err := os.ReadFile("migrations/1000006_source_evidence.up.sql")
	require.NoError(t, err)
	text := string(old)
	start := strings.Index(text, "CREATE TABLE source_captures (")
	end := strings.Index(text[start:], "CREATE TABLE source_capture_profiles") + start
	columns := "uuid,post_uuid,revision_uuid,origin,platform,captured_at,extractor_version,retention_policy,patch_digest,signature"
	_, err = raw.Exec(`PRAGMA foreign_keys=OFF; BEGIN;
 CREATE TABLE capture_time_fixture_rows AS SELECT ` + columns + ` FROM source_captures;
 DROP TABLE source_captures; ` + text[start:end] + `
 INSERT INTO source_captures SELECT * FROM capture_time_fixture_rows;
 DROP TABLE capture_time_fixture_rows;
 CREATE UNIQUE INDEX source_captures_scope ON source_captures(post_uuid,uuid);
 CREATE TRIGGER source_capture_immutable BEFORE UPDATE ON source_captures
 BEGIN SELECT RAISE(ABORT,'source captures are immutable'); END;
 CREATE TRIGGER source_capture_active_post BEFORE INSERT ON source_captures
 WHEN EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state!='active')
 BEGIN SELECT RAISE(ABORT,'forgotten source post cannot receive captures'); END;
 DELETE FROM native_migration_history WHERE version=1000061;
 COMMIT; PRAGMA foreign_keys=ON;`)
	require.NoError(t, err)
}

func TestCaptureRecordingTimeMigrationPreservesExistingObservationsAndRollsBackCollision(t *testing.T) {
	config.InitializeEmpty()
	fixture, repo := archiveTestDatabase(t)
	capture := publisherCapture(t, repo, "native:twitter", "twitter", `{"category":"twitter","tweet_id":"one","content":"Retained source text"}`)
	for _, collision := range []bool{false, true} {
		name := "upgrade"
		if collision {
			name = "collision"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "schema60.sqlite")
			buildLegacyDatabase(t, path, 86, true)
			db := sqlite.NewDatabase()
			defer db.Close()
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(path), &needed))
			m, err := sqlite.NewMigrator(db)
			require.NoError(t, err)
			for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+60; version = m.CurrentSchemaVersion() {
				require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(version)))
			}
			m.Close()
			raw := openRawDB(t, path)
			defer raw.Close()
			_, err = raw.Exec("ATTACH DATABASE ? AS source_fixture", fixture.DatabasePath())
			require.NoError(t, err)
			tx, err := raw.Begin()
			require.NoError(t, err)
			for _, table := range []string{"source_posts", "source_post_identifiers", "source_payloads", "source_profile_bodies", "source_post_revisions", "source_captures", "source_capture_profiles"} {
				copyHistoricalFixtureTable(t, tx, table)
			}
			require.NoError(t, tx.Commit())
			_, err = raw.Exec("DETACH DATABASE source_fixture")
			require.NoError(t, err)
			// A previous capture may have been removed; rebuilding the table must
			// retain surviving rowids as well as the public UUIDs and signed data.
			var immutableTrigger string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='source_capture_immutable'").Scan(&immutableTrigger))
			_, err = raw.Exec("DROP TRIGGER source_capture_immutable; UPDATE source_captures SET rowid=101; " + immutableTrigger)
			require.NoError(t, err)
			before := albumJobRows(t, raw, "source_captures")
			if collision {
				_, err = raw.Exec("CREATE TABLE native_capture_time_rows(retained TEXT); INSERT INTO native_capture_time_rows VALUES('Original unrelated data')")
				require.NoError(t, err)
				require.Error(t, db.RunAllMigrations())
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty=1"))
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM native_capture_time_rows WHERE retained='Original unrelated data'"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_table_info('source_captures') WHERE name='recorded_at'"))
			} else {
				require.NoError(t, db.RunAllMigrations())
				require.NoError(t, db.ReInitialise())
				upgraded := db.Repository()
				require.NoError(t, upgraded.WithReadTxn(t.Context(), func(ctx context.Context) error {
					got, err := upgraded.SourceEvidence.FindCapture(ctx, capture.UUID)
					require.Equal(t, capture, got)
					return err
				}))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_captures WHERE recorded_at IS NOT NULL"))
			}
			require.Equal(t, before, albumJobRows(t, raw, "source_captures"))
			require.EqualValues(t, 101, queryUint(t, raw, "SELECT rowid FROM source_captures"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}
