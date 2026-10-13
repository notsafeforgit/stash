package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestRecoveryChecksActualMediaBeforeChangingAssociations(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "clip.mkv"), []byte("video"), 0600))
	preview := &sqlite.LegacyTwitterRecoveryPreview{Slots: []sqlite.LegacyTwitterSlot{{LibraryPath: "/media/clip.mkv", Size: 5}}}
	require.NoError(t, checkFiles(preview, "/media", directory))
	preview.Slots[0].Size = 8
	require.Error(t, checkFiles(preview, "/media", directory))
	preview.Slots[0].LibraryPath = "/outside/clip.mkv"
	require.Error(t, checkFiles(preview, "/media", directory))
	preview.Slots[0].LibraryPath = "/media/missing.mkv"
	require.Error(t, checkFiles(preview, "/media", directory))
}
