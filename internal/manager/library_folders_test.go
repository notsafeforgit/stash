package manager

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestLibraryFolderImportSharesRootAndPreservesLaterEdits(t *testing.T) {
	m := defaultFilterTestManager(t, "")
	ctx := context.Background()
	path := t.TempDir()
	binding, err := archive.ProbeMediaRoot(path)
	require.NoError(t, err)
	var original *models.MediaRoot
	require.NoError(t, m.Repository.WithTxn(ctx, func(ctx context.Context) error {
		original, err = m.Repository.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Downloads", State: "active", Binding: binding}})
		return err
	}))
	m.Config.SetInterface(config.Stash, config.StashConfigs{{Path: path, ExcludeVideo: true}})
	require.NoError(t, m.Config.Write())
	require.NoError(t, m.PromoteLibraryFolderConfig(ctx))
	require.Empty(t, m.Config.GetStashPaths())
	var imported *models.MediaRoot
	require.NoError(t, m.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
		roots, err := libraryRoots(ctx, m.Repository)
		require.NoError(t, err)
		require.Len(t, roots, 1)
		imported = roots[0]
		return nil
	}))
	require.Equal(t, original.UUID, imported.UUID)
	require.Equal(t, original.Revision, imported.Revision, "importing scan options must not invalidate scrape leases")
	require.Equal(t, binding, imported.Binding)
	require.Equal(t, &models.MediaRootScan{Revision: 1, Enabled: true, ExcludeVideo: true}, imported.Scan)
	folders, err := LibraryPaths(ctx, m.Repository)
	require.NoError(t, err)
	require.Equal(t, config.StashConfigs{{Path: path, ExcludeVideo: true}}, folders)
	require.NoError(t, m.Repository.WithTxn(ctx, func(ctx context.Context) error {
		definition := imported.MediaRootDefinition
		definition.Scan = &models.MediaRootScan{Revision: imported.Scan.Revision, Enabled: true}
		_, err := m.Repository.MediaRoot.Put(ctx, models.MediaRootInput{UUID: imported.UUID, ExpectedRevision: imported.Revision, Origin: "review", MediaRootDefinition: definition})
		return err
	}))
	// Reopening the database must not restore the old YAML exclusions.
	dbPath := m.Database.DatabasePath()
	require.NoError(t, m.Database.Close())
	require.NoError(t, m.Database.Open(dbPath))
	m.Repository = m.Database.Repository()
	require.NoError(t, m.PromoteLibraryFolderConfig(ctx))
	folders, err = LibraryPaths(ctx, m.Repository)
	require.NoError(t, err)
	require.False(t, folders[0].ExcludeVideo)
}

func TestLibraryFolderImportResumesAfterConfigPublicationFailure(t *testing.T) {
	m := defaultFilterTestManager(t, "")
	ctx := context.Background()
	offline := filepath.Join(t.TempDir(), "not-mounted")
	m.Config.SetInterface(config.Stash, config.StashConfigs{{Path: offline, ExcludeImage: true}})
	configPath := filepath.Join(t.TempDir(), "missing", "config.yml")
	m.Config.SetConfigFile(configPath)
	require.Error(t, m.PromoteLibraryFolderConfig(ctx))
	require.Len(t, m.Config.GetStashPaths(), 1)
	require.NoError(t, os.Mkdir(filepath.Dir(configPath), 0700))
	require.NoError(t, m.PromoteLibraryFolderConfig(ctx))
	require.NoError(t, m.PromoteLibraryFolderConfig(ctx))
	require.NoError(t, m.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
		roots, err := libraryRoots(ctx, m.Repository)
		require.NoError(t, err)
		require.Len(t, roots, 1)
		require.Equal(t, offline, roots[0].UnboundPath)
		require.Nil(t, roots[0].Binding)
		return nil
	}))
	folders, err := LibraryPaths(ctx, m.Repository)
	require.NoError(t, err)
	require.Equal(t, config.StashConfigs{{Path: offline, ExcludeImage: true, ScanDisabled: true}}, folders)
	require.Empty(t, getScanPaths(nil, folders))
	require.Empty(t, getScanPaths([]string{offline}, folders))
}

func TestLibraryFolderImportRejectsConflictingDuplicatePaths(t *testing.T) {
	m := defaultFilterTestManager(t, "")
	path := t.TempDir()
	m.Config.SetInterface(config.Stash, config.StashConfigs{{Path: path}, {Path: path + string(os.PathSeparator), ExcludeVideo: true}})
	require.ErrorContains(t, m.PromoteLibraryFolderConfig(context.Background()), "conflicting scan options")
	require.NoError(t, m.Repository.WithReadTxn(context.Background(), func(ctx context.Context) error {
		roots, err := libraryRoots(ctx, m.Repository)
		require.NoError(t, err)
		require.Empty(t, roots, "the whole configuration import rolls back")
		return nil
	}))
}
