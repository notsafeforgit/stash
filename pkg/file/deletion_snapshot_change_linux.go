package file

import (
	"fmt"
	"os"
	"syscall"
)

func deletionSnapshotChangeToken(info os.FileInfo) string {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d:%d", stat.Ctim.Sec, stat.Ctim.Nsec)
	}
	return ""
}
