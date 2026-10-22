package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestPersistentFileIdentitySurvivesDeviceRenumbering(t *testing.T) {
	info := unix.Stat_t{Dev: 44, Ino: 1920}
	filesystem := unix.Statfs_t{Type: 0x2fc12fc1, Fsid: unix.Fsid{Val: [2]int32{123, -456}}} // ZFS_SUPER_MAGIC
	before, err := persistentFileIdentity(info, filesystem)
	require.NoError(t, err)
	info.Dev = 45
	after, err := persistentFileIdentity(info, filesystem)
	require.NoError(t, err)
	require.Equal(t, before, after, "reboot allocation must not change the filesystem identity")

	otherFilesystem := filesystem
	otherFilesystem.Fsid.Val[0]++
	replacement, err := persistentFileIdentity(info, otherFilesystem)
	require.NoError(t, err)
	require.NotEqual(t, before, replacement, "an equal inode on another filesystem is not the reviewed file")
	otherFilesystem = filesystem
	otherFilesystem.Type = unix.EXT4_SUPER_MAGIC
	replacement, err = persistentFileIdentity(info, otherFilesystem)
	require.NoError(t, err)
	require.NotEqual(t, before, replacement)
	info.Ino++
	replacement, err = persistentFileIdentity(info, filesystem)
	require.NoError(t, err)
	require.NotEqual(t, before, replacement, "a replacement directory must still require review")
	_, err = persistentFileIdentity(info, unix.Statfs_t{})
	require.ErrorContains(t, err, "persistent identity")
}

func TestPersistentFileIdentityUsesOpenedDescriptor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "media")
	require.NoError(t, os.Mkdir(path, 0700))
	opened, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, opened.Close()) })
	identity, err := OpenPersistentFileIdentity(opened)
	require.NoError(t, err)
	require.Contains(t, identity, PersistentFileIdentityPrefix)
	require.NoError(t, os.Rename(path, path+"-old"))
	require.NoError(t, os.Mkdir(path, 0700))
	still, err := OpenPersistentFileIdentity(opened)
	require.NoError(t, err)
	require.Equal(t, identity, still)
	replacement, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, replacement.Close()) })
	changed, err := OpenPersistentFileIdentity(replacement)
	require.NoError(t, err)
	require.NotEqual(t, identity, changed)
}
