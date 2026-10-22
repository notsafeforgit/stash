package fsutil

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// OpenPersistentFileIdentity identifies an opened inode within its filesystem.
// st_dev is allocated at mount time and can change after a normal reboot (for
// example on ZFS). f_fsid identifies the filesystem independently of that
// allocation. Reading both through the descriptor avoids pathname races and
// requires neither host mount commands nor access outside a container's mounts.
func OpenPersistentFileIdentity(file *os.File) (string, error) {
	var info unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &info); err != nil {
		return "", err
	}
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(int(file.Fd()), &filesystem); err != nil {
		return "", err
	}
	return persistentFileIdentity(info, filesystem)
}

func persistentFileIdentity(info unix.Stat_t, filesystem unix.Statfs_t) (string, error) {
	if filesystem.Fsid.Val == [2]int32{} {
		return "", errors.New("filesystem does not provide a persistent identity")
	}
	return fmt.Sprintf("%s%x:%08x%08x:%x", PersistentFileIdentityPrefix,
		uint32(filesystem.Type), uint32(filesystem.Fsid.Val[0]), uint32(filesystem.Fsid.Val[1]), info.Ino), nil
}
