package ffmpeg

import (
	"context"
	"os"
)

func openedProbeInput(ctx context.Context, file *os.File, size int64) (string, []*os.File, func(), error) {
	// ExtraFiles installs the same open file in the child as descriptor 3. The
	// proc descriptor path is seekable, including MP4 files with a trailing moov.
	return "/proc/self/fd/3", []*os.File{file}, func() {}, nil
}
