package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeMembershipCleanupSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeSavedShareLinkSchema(t, raw)
	_, err := raw.Exec(`DROP TRIGGER IF EXISTS catalog_membership_compaction_guard;
DROP TABLE IF EXISTS source_post_import_labels; DROP TABLE IF EXISTS source_post_studios;
DROP INDEX IF EXISTS catalog_membership_records_collection; DROP INDEX IF EXISTS catalog_membership_records_membership;
DROP INDEX IF EXISTS translation_targets_collection; DROP INDEX IF EXISTS automation_translation_records_collection;
DROP INDEX IF EXISTS automation_enrichment_records_collection; DROP INDEX IF EXISTS automation_enrichment_records_alias;
DROP INDEX IF EXISTS automation_discovery_records_collection; DROP INDEX IF EXISTS ingest_receipts_collection;
DELETE FROM native_migration_history WHERE version=1000121`)
	require.NoError(t, err)
	if queryUint(t, raw, "SELECT count(*) FROM pragma_table_info('catalog_snapshots') WHERE name='memberships_compacted'") != 0 {
		_, err := raw.Exec("ALTER TABLE catalog_snapshots DROP COLUMN memberships_compacted")
		require.NoError(t, err)
	}
}

func TestCatalogMembershipCleanupMigrationIsAtomicAndPreservesConfiguredSources(t *testing.T) {
	// Exactly one batch leaves a resumable input before the completion call.
	// Seed its valid historical records into an actual schema 1000120, rather
	// than relabelling a database that already ran the migration under test.
	f := membershipImportFixture(t, 50, nil)
	advanceMembershipImport(t, f, 0)
	attachmentSQL(t, f.db, "UPDATE catalog_membership_imports SET state='mapped'")
	path := filepath.Join(t.TempDir(), "before-cleanup.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+120; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	defer raw.Close()
	copyMembershipFixtureData(t, raw, f.db.DatabasePath())
	var configured, oldGroup string
	require.NoError(t, raw.QueryRow("SELECT collection_uuid FROM catalog_snapshots LIMIT 1").Scan(&configured))
	require.NoError(t, raw.QueryRow("SELECT collection_uuid FROM catalog_membership_groups LIMIT 1").Scan(&oldGroup))
	counts := map[string]uint{}
	for _, table := range []string{"source_captures", "source_post_revisions", "source_posts", "source_accounts", "files", "scenes", "images", "galleries", "catalog_snapshots"} {
		counts[table] = queryUint(t, raw, "SELECT count(*) FROM "+table)
	}
	_, err = raw.Exec(`CREATE TRIGGER fail_membership_cleanup BEFORE DELETE ON source_collections
BEGIN SELECT RAISE(ABORT,'injected migration failure'); END`)
	require.NoError(t, err)
	require.ErrorContains(t, db.RunAllMigrations(), "injected migration failure")
	require.EqualValues(t, 1000120, queryUint(t, raw, "SELECT version FROM schema_migrations"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='source_post_import_labels'"))
	require.EqualValues(t, 50, queryUint(t, raw, "SELECT count(*) FROM catalog_membership_records"))
	_, err = raw.Exec("DROP TRIGGER fail_membership_cleanup")
	require.NoError(t, err)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	for table, count := range counts {
		require.Equal(t, count, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	require.EqualValues(t, 50, queryUint(t, raw, "SELECT count(*) FROM source_post_import_labels"))
	repo := db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		removed, err := repo.SourceCollection.Find(ctx, oldGroup)
		require.NoError(t, err)
		require.Nil(t, removed)
		kept, err := repo.SourceCollection.Find(ctx, configured)
		require.NotNil(t, kept)
		return err
	}))
	require.NoError(t, db.AuditForTesting(path))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

// Only fixture data crosses this boundary. Every schema object and migration
// receipt in the target was constructed by running the historical migrations.
func copyMembershipFixtureData(t *testing.T, raw *sql.DB, source string) {
	t.Helper()
	quote := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	_, err := raw.Exec("PRAGMA foreign_keys=OFF; ATTACH DATABASE ? AS fixture", source)
	require.NoError(t, err)
	tx, err := sqlx.NewDb(raw, "sqlite3").Beginx()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	var guards []struct {
		Name string
		SQL  string
	}
	require.NoError(t, tx.Select(&guards, "SELECT name,sql FROM sqlite_schema WHERE type='trigger'"))
	for _, guard := range guards {
		_, err := tx.Exec("DROP TRIGGER " + quote(guard.Name))
		require.NoError(t, err)
	}
	var tables []string
	require.NoError(t, tx.Select(&tables, "SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT IN ('schema_migrations','native_migration_history')"))
	for _, table := range tables {
		var columns []string
		require.NoError(t, tx.Select(&columns, "SELECT name FROM pragma_table_xinfo(?) WHERE hidden=0", table))
		for i, column := range columns {
			columns[i] = quote(column)
		}
		cols := strings.Join(columns, ",")
		_, err = tx.Exec("DELETE FROM " + quote(table))
		require.NoError(t, err)
		_, err = tx.Exec("INSERT INTO " + quote(table) + "(" + cols + ") SELECT " + cols + " FROM fixture." + quote(table))
		require.NoError(t, err, table)
	}
	for _, guard := range guards {
		_, err := tx.Exec(guard.SQL)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
	_, err = raw.Exec("DETACH DATABASE fixture; PRAGMA foreign_keys=ON")
	require.NoError(t, err)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestCatalogMembershipCompletionRemovesImportGroupsAndPreservesPostData(t *testing.T) {
	f := membershipImportFixture(t, 55, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	account := createSourceAccount(t, f.repo, "native:reddit")
	observeAccount(t, f.repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "handle", Value: "publisher"}, accountEvidence())
	untouched := map[string]uint{}
	for _, table := range []string{"source_posts", "source_post_revisions", "source_captures", "source_attachments", "source_accounts", "capture_publisher_decisions", "post_media_links", "files", "images", "scenes", "galleries"} {
		untouched[table] = queryUint(t, raw, "SELECT count(*) FROM "+table)
	}
	first := advanceMembershipImport(t, f, 0)
	require.Equal(t, "running", first.State)
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM catalog_membership_groups"))
	attachmentSQL(t, f.db, "INSERT INTO studios(id,name,created_at,updated_at) VALUES(191,'Source studio',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)")
	studio := archiveFind(t, f.repo, models.ArchiveStudio, 191)
	var group, post string
	require.NoError(t, raw.QueryRow("SELECT collection_uuid,post_uuid FROM catalog_membership_records ORDER BY ordinal LIMIT 1").Scan(&group, &post))
	assignSourceStudio(t, f.repo, group, 0, &studio.UUID)
	var before *models.CatalogAssociationPreview
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		before, err = f.repo.CapturePublisher.PreviewCatalogAssociation(ctx, post)
		return err
	}))
	// Inject a late failure: no copied hints, removed group or completion flag
	// may escape a rolled-back import transaction.
	attachmentSQL(t, f.db, `CREATE TRIGGER fail_membership_cleanup BEFORE DELETE ON source_collections
WHEN OLD.uuid='`+group+`' BEGIN SELECT RAISE(ABORT,'injected cleanup failure'); END`)
	require.ErrorContains(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogMembershipImport.Advance(ctx, f.manifest.UUID, f.sha, first.LastOrdinal, catalogImportNow)
		return err
	}), "injected cleanup failure")
	require.EqualValues(t, 50, queryUint(t, raw, "SELECT count(*) FROM catalog_membership_records"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_post_import_labels"))
	attachmentSQL(t, f.db, "DROP TRIGGER fail_membership_cleanup")
	result := advanceMembershipImport(t, f, first.LastOrdinal)
	require.Equal(t, "mapped", result.State)
	require.EqualValues(t, 55, result.MappedRecords)
	require.Equal(t, result, advanceMembershipImport(t, f, result.LastOrdinal))
	for _, table := range []string{"catalog_membership_records", "catalog_membership_groups", "source_collection_post_evidence"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_snapshot_records WHERE source_table='memberships'"))
	require.EqualValues(t, 55, queryUint(t, raw, "SELECT count(*) FROM source_post_import_labels"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM catalog_snapshots WHERE memberships_compacted=1"))
	for table, count := range untouched {
		require.Equal(t, count, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := f.repo.SourceCollection.Find(ctx, group)
		require.NoError(t, err)
		require.Nil(t, current)
		studios, err := f.repo.SourceStudio.ForPost(ctx, post)
		require.NoError(t, err)
		require.Equal(t, []models.SourceStudio{{UUID: studio.UUID, Name: "Source studio"}}, studios)
		after, err := f.repo.CapturePublisher.PreviewCatalogAssociation(ctx, post)
		require.Equal(t, before, after)
		require.Contains(t, after.Candidates, account.UUID)
		return err
	}))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, f.db.AuditForTesting(f.db.DatabasePath()))
	_, err := raw.Exec("UPDATE catalog_snapshots SET memberships_compacted=0")
	require.Error(t, err)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}
