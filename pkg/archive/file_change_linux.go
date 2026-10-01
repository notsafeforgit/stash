package archive

import (
	"fmt"
	"io/fs"
	"syscall"
)

// Linux ctime catches in-place writes followed by a restored modification time.
// Access time is excluded because reading the media can change it itself.
func fileChangeToken(info fs.FileInfo) string {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d:%d", stat.Ctim.Sec, stat.Ctim.Nsec)
	}
	return ""
}
