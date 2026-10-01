//go:build !linux

package ffmpeg

import (
	"context"
	"io"
	"os"
)

type probeCopyReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r probeCopyReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func openedProbeInput(ctx context.Context, file *os.File, size int64) (string, []*os.File, func(), error) {
	// Platforms without Linux's seekable inherited-descriptor path use a private
	// snapshot. Never fall back to reopening the unverified source pathname.
	tmp, err := os.CreateTemp("", "stash-opened-probe-*")
	if err != nil {
		return "", nil, nil, err
	}
	cleanup := func() { os.Remove(tmp.Name()) }
	n, err := io.Copy(tmp, probeCopyReader{ctx: ctx, reader: io.NewSectionReader(file, 0, size)})
	closeErr := tmp.Close()
	if err == nil && n != size {
		err = io.ErrUnexpectedEOF
	}
	if err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return "", nil, nil, err
	}
	return tmp.Name(), nil, cleanup, nil
}
