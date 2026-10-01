package archive

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMediaRootOpensOnlyReviewedRegularFiles(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "media")
	require.NoError(t, os.Mkdir(rootPath, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(rootPath, "clip.mp4"), []byte("original media"), 0600))
	binding, err := ProbeMediaRoot(rootPath)
	require.NoError(t, err)
	root := models.MediaRoot{MediaRootDefinition: models.MediaRootDefinition{State: "active", Binding: binding}}
	file, err := OpenMediaRootFile(root, "clip.mp4")
	require.NoError(t, err)
	defer file.Close()
	for _, name := range []string{"../clip.mp4", "/clip.mp4", ".", "a/../clip.mp4", "a//b", "a\\b", "clip.mp4.part", "clip.MP4.PART", "a\x00b", "missing.mp4"} {
		_, err := OpenMediaRootFile(root, name)
		require.Error(t, err, name)
	}
	require.NoError(t, os.Mkdir(filepath.Join(rootPath, "directory.mp4"), 0700))
	_, err = OpenMediaRootFile(root, "directory.mp4")
	require.ErrorContains(t, err, "regular file")
	for _, state := range []string{"disabled", "retired", ""} {
		inactive := root
		inactive.State = state
		_, err := OpenMediaRootFile(inactive, "clip.mp4")
		require.Error(t, err)
	}
	unbound := root
	unbound.Binding = nil
	_, err = OpenMediaRootFile(unbound, "clip.mp4")
	require.Error(t, err)
	// An absent mount replaced by a different directory must not pass readiness.
	require.NoError(t, os.Rename(rootPath, rootPath+"-moved"))
	require.NoError(t, os.Mkdir(rootPath, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(rootPath, "clip.mp4"), []byte("replacement"), 0600))
	require.ErrorContains(t, VerifyMediaRootBinding(*binding), "directory changed")
	_, err = OpenMediaRootFile(root, "clip.mp4")
	require.ErrorContains(t, err, "directory changed")
	// The file descriptor opened before the rename still refers to the original.
	body, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, "original media", string(body))
	moved, err := ProbeMediaRoot(rootPath + "-moved")
	require.NoError(t, err)
	require.Equal(t, binding.DirectoryIdentity, moved.DirectoryIdentity)
}

func TestMediaRootSymlinkConfinement(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "media")
	require.NoError(t, os.Mkdir(rootPath, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(rootPath, "inside.mp4"), []byte("inside"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "outside.mp4"), []byte("outside"), 0600))
	if err := os.Symlink("inside.mp4", filepath.Join(rootPath, "internal.mp4")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	require.NoError(t, os.Symlink("../outside.mp4", filepath.Join(rootPath, "escape.mp4")))
	require.NoError(t, os.Symlink(dir, filepath.Join(rootPath, "escape-directory")))
	binding, err := ProbeMediaRoot(rootPath)
	require.NoError(t, err)
	root := models.MediaRoot{MediaRootDefinition: models.MediaRootDefinition{State: "active", Binding: binding}}
	file, err := OpenMediaRootFile(root, "internal.mp4")
	require.NoError(t, err)
	file.Close()
	for _, name := range []string{"escape.mp4", "escape-directory/outside.mp4"} {
		_, err := OpenMediaRootFile(root, name)
		require.Error(t, err, name)
	}
}

func TestMediaRootRelativePathsAreCanonical(t *testing.T) {
	for _, value := range []string{"folder/movie.mp4", "purchased/Creator (2026)", "é/标题.jpg"} {
		require.True(t, ValidRootRelativePath(value, false), value)
	}
	require.True(t, ValidRootRelativePath(".", true))
	for _, value := range []string{"", "..", "../x", "a/..", "a/.", "a/", "a//b", "a\\b", "/absolute", "bad\nfile", "\xff"} {
		require.False(t, ValidRootRelativePath(value, true), value)
	}
	_, err := ProbeMediaRoot("relative")
	require.Error(t, err)
}
