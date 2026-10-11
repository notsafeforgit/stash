package file

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCleanerPreservesProtectedOfflineFilesAndFolders(t *testing.T) {
	// There is deliberately no filesystem or repository: a protected offline
	// folder must be skipped before either is accessed or deletion is queued.
	j := &cleanJob{options: CleanOptions{SkipPath: func(string) bool { return true }}}
	media := &models.VideoFile{BaseFile: &models.BaseFile{ID: 5, Path: "/offline/item.mp4"}}
	folder := &models.Folder{ID: 6, Path: "/offline"}
	require.False(t, j.shouldClean(context.Background(), media))
	require.False(t, j.shouldCleanFolder(context.Background(), folder))
	deletions := newDeleteSet()
	require.NoError(t, j.flagFileForDelete(context.Background(), &deletions, media))
	require.NoError(t, j.flagFolderForDelete(context.Background(), &deletions, folder))
	require.Zero(t, deletions.len())
}
