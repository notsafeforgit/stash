package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenameNoReplacePreservesOccupiedDestination(t *testing.T) {
	for _, kind := range []string{"file", "directory", "dangling symlink"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			source, destination := filepath.Join(parent, "source"), filepath.Join(parent, "destination")
			require.NoError(t, os.WriteFile(source, []byte("original"), 0644))
			switch kind {
			case "file":
				require.NoError(t, os.WriteFile(destination, []byte("unrelated"), 0644))
			case "directory":
				require.NoError(t, os.Mkdir(destination, 0755))
			case "dangling symlink":
				if err := os.Symlink("missing", destination); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			before, err := FileIdentity(destination)
			require.NoError(t, err)
			require.Error(t, RenameNoReplace(source, destination))
			after, err := FileIdentity(destination)
			require.NoError(t, err)
			require.Equal(t, before, after)
			data, err := os.ReadFile(source)
			require.NoError(t, err)
			require.Equal(t, "original", string(data))
		})
	}
}

func TestCopyPathDurablePreservesSourceAndRejectsOverwrite(t *testing.T) {
	parent := t.TempDir()
	source, destination := filepath.Join(parent, "source"), filepath.Join(parent, "destination")
	require.NoError(t, os.Mkdir(source, 0750))
	require.NoError(t, os.WriteFile(filepath.Join(source, "child"), []byte("contents"), 0640))
	require.NoError(t, CopyPathDurable(source, destination))
	for _, path := range []string{source, destination} {
		data, err := os.ReadFile(filepath.Join(path, "child"))
		require.NoError(t, err)
		require.Equal(t, "contents", string(data))
	}
	require.Error(t, CopyPathDurable(source, destination))
	require.Error(t, CopyPathDurable(filepath.Join(source, "child"), filepath.Join(destination, "child")))
}
