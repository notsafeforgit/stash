package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMediaBindingRetainsIdentityFormatForSavedWork(t *testing.T) {
	root, path := verifiedMediaFixture(t)
	require.True(t, strings.HasPrefix(root.Binding.DirectoryIdentity, fsutil.PersistentFileIdentityPrefix))
	stable, err := InspectMediaFile(t.Context(), root, filepath.Base(path))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(stable.Snapshot.Identity, fsutil.PersistentFileIdentityPrefix))

	directory, err := os.Open(root.Binding.Path)
	require.NoError(t, err)
	legacyRootID, err := fsutil.OpenFileIdentity(directory)
	require.NoError(t, err)
	require.NoError(t, directory.Close())
	legacy := root
	legacy.Binding = &models.MediaRootBinding{Path: root.Binding.Path, DirectoryIdentity: legacyRootID}
	require.NoError(t, VerifyMediaRootBinding(*legacy.Binding))
	old, err := InspectMediaFile(t.Context(), legacy, filepath.Base(path))
	require.NoError(t, err)
	require.False(t, strings.HasPrefix(old.Snapshot.Identity, fsutil.PersistentFileIdentityPrefix))
	require.NotEqual(t, old.Snapshot.Identity, stable.Snapshot.Identity)
	verified, err := VerifyMediaFile(t.Context(), legacy, filepath.Base(path), nil, "")
	require.NoError(t, err)
	defer verified.Close()
	require.True(t, old.Matches(verified.Snapshot, verified.SHA256), "retained legacy work must still verify against its original snapshot format")
	require.NoError(t, verified.Revalidate(t.Context(), legacy))
	for _, bound := range []models.MediaRoot{root, legacy} {
		page, err := ReadMediaDirectory(t.Context(), bound, MediaDirectoryInput{Directory: ".", Limit: 50})
		require.NoError(t, err)
		repeated, err := ReadMediaDirectory(t.Context(), bound, MediaDirectoryInput{Directory: ".", Limit: 50})
		require.NoError(t, err)
		require.Equal(t, page.Signature, repeated.Signature)
	}

	// A legacy mismatch has no evidence authorizing a different mount. Upgrading
	// the binary must never automatically accept it or discard its saved work.
	parts := strings.Split(legacyRootID, ":")
	legacy.Binding.DirectoryIdentity = "ffffffffffffffff:" + parts[1]
	require.ErrorContains(t, VerifyMediaRootBinding(*legacy.Binding), "directory changed")
	_, err = InspectMediaFile(t.Context(), legacy, filepath.Base(path))
	require.Error(t, err)

	// After an explicit same-root review, use the persistent mount check but
	// keep the original device namespace for retained snapshots and pagination.
	reviewed, err := UpgradeMediaRootIdentity(*legacy.Binding, *root.Binding)
	require.NoError(t, err)
	legacy.Binding = reviewed
	inspection, err := InspectMediaFile(t.Context(), legacy, filepath.Base(path))
	require.NoError(t, err)
	_, inode, _ := strings.Cut(old.Snapshot.Identity, ":")
	require.Equal(t, "ffffffffffffffff:"+inode, inspection.Snapshot.Identity)
	legacy.Binding.DirectoryIdentity = legacyRootID
	after, err := InspectMediaFile(t.Context(), legacy, filepath.Base(path))
	require.NoError(t, err)
	require.Equal(t, old, after, "an upgrade preserves saved file fingerprints exactly")
	legacy.Binding.PersistentIdentity = ""
	page, err := ReadMediaDirectory(t.Context(), legacy, MediaDirectoryInput{Directory: ".", Limit: 50})
	require.NoError(t, err)
	legacy.Binding.PersistentIdentity = root.Binding.DirectoryIdentity
	upgradedPage, err := ReadMediaDirectory(t.Context(), legacy, MediaDirectoryInput{Directory: ".", Limit: 50})
	require.NoError(t, err)
	require.Equal(t, page, upgradedPage)
	verified, err = VerifyMediaFile(t.Context(), legacy, filepath.Base(path), nil, "")
	require.NoError(t, err)
	defer verified.Close()
	require.True(t, old.Matches(verified.Snapshot, verified.SHA256))
	require.NoError(t, verified.Revalidate(t.Context(), legacy))

	// The upgrade does not permit a subsequently replaced directory.
	require.NoError(t, os.Rename(root.Binding.Path, root.Binding.Path+"-old"))
	require.NoError(t, os.Mkdir(root.Binding.Path, 0700))
	require.Error(t, VerifyMediaRootBinding(*legacy.Binding))
}
