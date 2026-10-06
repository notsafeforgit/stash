package archive_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMediaDirectoryPagesSupportedFilesWithoutFollowingSymlinks(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"z.mp4", "a.jpg", "B.JPG", "zero.mp4", "sound.m4a", "download.mp4.PART", "notes.json", "with%percent.mp4"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name), 0600))
	}
	require.NoError(t, os.Mkdir(filepath.Join(dir, "folder"), 0700))
	require.NoError(t, os.Symlink(filepath.Join(dir, "a.jpg"), filepath.Join(dir, "alias.jpg")))
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "outside")))
	binding, err := archive.ProbeMediaRoot(dir)
	require.NoError(t, err)
	root := models.MediaRoot{UUID: uuid.NewString(), Revision: 1, MediaRootDefinition: models.MediaRootDefinition{State: "active", Binding: binding}}
	input := archive.MediaDirectoryInput{Directory: ".", Scope: "collection:1", Limit: 2, Extensions: map[string]models.ArchiveEntityKind{"mp4": models.ArchiveScene, "jpg": models.ArchiveImage}}
	first, err := archive.ReadMediaDirectory(t.Context(), root, input)
	require.NoError(t, err)
	require.Equal(t, []string{"folder", "B.JPG"}, []string{first.Entries[0].Name, first.Entries[1].Name})
	require.Equal(t, "directory", first.Entries[0].Kind)
	require.Equal(t, "image", first.Entries[1].Kind)
	require.Equal(t, "1/B.JPG", first.NextAfter)
	input.After, input.Signature = first.NextAfter, first.Signature
	second, err := archive.ReadMediaDirectory(t.Context(), root, input)
	require.NoError(t, err)
	require.Equal(t, first.Signature, second.Signature)
	require.Equal(t, []string{"a.jpg", "with%percent.mp4"}, []string{second.Entries[0].Name, second.Entries[1].Name})
	input.After = second.NextAfter
	third, err := archive.ReadMediaDirectory(t.Context(), root, input)
	require.NoError(t, err)
	require.Empty(t, third.NextAfter)
	require.Equal(t, []string{"z.mp4", "zero.mp4"}, []string{third.Entries[0].Name, third.Entries[1].Name})
	input.Scope = "collection:2"
	_, err = archive.ReadMediaDirectory(t.Context(), root, input)
	require.ErrorIs(t, err, archive.ErrMediaDirectoryChanged)
	input.Scope, input.After, input.Signature, input.Query = "collection:1", "", "", "%"
	filtered, err := archive.ReadMediaDirectory(t.Context(), root, input)
	require.NoError(t, err)
	require.Len(t, filtered.Entries, 1)
	require.Equal(t, "with%percent.mp4", filtered.Entries[0].Name)
	input.Query, input.After, input.Signature = "", first.NextAfter, first.Signature
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.mp4"), []byte("new"), 0600))
	_, err = archive.ReadMediaDirectory(t.Context(), root, input)
	require.ErrorIs(t, err, archive.ErrMediaDirectoryChanged)
	input.After, input.Signature, input.Directory = "", "", "outside"
	_, err = archive.ReadMediaDirectory(t.Context(), root, input)
	require.Error(t, err, "directory symlinks cannot escape the reviewed media root")
}

func TestMediaDirectoryRejectsInvalidPagesChangedMountsAndCancelledReads(t *testing.T) {
	dir := t.TempDir()
	binding, err := archive.ProbeMediaRoot(dir)
	require.NoError(t, err)
	root := models.MediaRoot{UUID: uuid.NewString(), Revision: 1, MediaRootDefinition: models.MediaRootDefinition{State: "active", Binding: binding}}
	input := archive.MediaDirectoryInput{Directory: ".", Scope: "collection:1", Limit: 2}
	for _, after := range []string{"a", "0/", "2/name", "0/../outside", "1/folder/file"} {
		bad := input
		bad.After, bad.Signature = after, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		_, err := archive.ReadMediaDirectory(t.Context(), root, bad)
		require.ErrorIs(t, err, archive.ErrMediaDirectoryInput, after)
	}
	for _, directory := range []string{"../outside", "/absolute", "x/../y"} {
		bad := input
		bad.Directory = directory
		_, err := archive.ReadMediaDirectory(t.Context(), root, bad)
		require.ErrorIs(t, err, archive.ErrMediaDirectoryInput)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = archive.ReadMediaDirectory(ctx, root, input)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, os.Rename(dir, dir+"-old"))
	t.Cleanup(func() { _ = os.RemoveAll(dir + "-old") })
	require.NoError(t, os.Mkdir(dir, 0700))
	_, err = archive.ReadMediaDirectory(t.Context(), root, input)
	require.Error(t, err, "a replaced mount must not expose the replacement directory")
}
