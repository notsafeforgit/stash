package sqlite_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func removeLibraryFolderSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`DROP TRIGGER media_root_scan_revision;
ALTER TABLE media_roots DROP COLUMN scan_revision;
ALTER TABLE media_roots DROP COLUMN scan_enabled;
ALTER TABLE media_roots DROP COLUMN exclude_video;
ALTER TABLE media_roots DROP COLUMN exclude_image;
ALTER TABLE media_root_revisions DROP COLUMN unbound_path;
DELETE FROM native_migration_history WHERE version=1000113;`)
	require.NoError(t, err)
}

func TestLibraryFolderScanOptionsPreserveStorageRevisionAndRejectStaleEdits(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	binding, err := archive.ProbeMediaRoot(t.TempDir())
	require.NoError(t, err)
	root := putMediaRoot(t, repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Downloads", State: "active", Binding: binding}})
	input := models.MediaRootInput{UUID: root.UUID, ExpectedRevision: root.Revision, Origin: "review", MediaRootDefinition: root.MediaRootDefinition}
	input.Scan = &models.MediaRootScan{Enabled: true, ExcludeImage: true}
	updated := putMediaRoot(t, repo, input)
	require.Equal(t, root.Revision, updated.Revision)
	require.Equal(t, 1, updated.Scan.Revision)
	require.True(t, updated.Scan.ExcludeImage)
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := repo.MediaRoot.Put(ctx, input); return err })
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
	// Existing binding clients that do not edit scan settings preserve them.
	input.Scan, input.Label = nil, "Renamed downloads"
	renamed := putMediaRoot(t, repo, input)
	require.Equal(t, updated.Scan, renamed.Scan)
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Duplicate", State: "active", Binding: binding}})
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
}

func TestLibraryFolderMigrationPreservesRunningDownload(t *testing.T) {
	f := newSourceRunFixture(t)
	running := f.claim(t, f.submit(t, f.request()))
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	removeLibraryFolderSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000112")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.Error(t, f.db.Open(path))
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.ReInitialise())
	raw = openRawDB(t, path)
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000113"))
	require.NoError(t, raw.Close())
	require.Equal(t, running, f.find(t, running.UUID))
	root := putMediaRoot(t, f.repo, models.MediaRootInput{UUID: f.root.UUID, ExpectedRevision: f.root.Revision, Origin: "review",
		MediaRootDefinition: models.MediaRootDefinition{Label: f.root.Label, State: f.root.State, Binding: f.root.Binding, Scan: &models.MediaRootScan{Enabled: true}}})
	require.Equal(t, f.root.Revision, root.Revision)
	// Source-run authorization still accepts the original pinned root revision.
	require.Equal(t, running, f.find(t, running.UUID))
	_, err = f.coordinator.Renew(t.Context(), f.token, running.Lease(), time.Minute)
	require.NoError(t, err)
}
