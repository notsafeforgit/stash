package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	stashExec "github.com/stashapp/stash/pkg/exec"
)

// Only self-contained media containers are admitted through native file intake.
// Playlists, concat manifests, devices and network protocols cannot make ffprobe
// open another source while inspecting an authorized descriptor.
const openedMediaFormats = "mov,matroska,webm,avi,mpeg,mpegts,asf,flv,ogg,rm,swf,wtv,mxf,mjpeg,h264,hevc,av1,gif,apng,jpeg_pipe,png_pipe,webp_pipe,bmp_pipe,tiff_pipe,jpegxl_pipe,jpegxl_anim"
const maxOpenedProbeJSON = 1 << 20

// Do not embed bytes.Buffer: its promoted ReadFrom would let io.Copy bypass
// Write and its limit when os/exec drains the subprocess pipe.
type boundedProbeOutput struct{ buffer bytes.Buffer }

func (w *boundedProbeOutput) Write(p []byte) (int, error) {
	if len(p) > maxOpenedProbeJSON-w.buffer.Len() {
		return 0, errors.New("opened media probe output exceeds 1 MiB")
	}
	return w.buffer.Write(p)
}

// NewVideoFileFromOpen probes the caller's already-open regular file. logicalPath
// is presentation metadata; it is never reopened to determine content or size.
func (f *FFProbe) NewVideoFileFromOpen(ctx context.Context, file *os.File, logicalPath string) (*VideoFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("media probe requires a regular file")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	input, files, cleanup, err := openedProbeInput(ctx, file, info.Size())
	if err != nil {
		return nil, err
	}
	defer cleanup()
	args := []string{"-v", "error", "-print_format", "json", "-show_format", "-show_streams", "-show_error", "-protocol_whitelist", "file", "-format_whitelist", openedMediaFormats}
	if f.version.major >= 5 {
		args = append(args, "-show_entries", "stream_side_data=rotation")
	}
	args = append(args, "-i", input)
	cmd := stashExec.CommandContext(ctx, f.path, args...)
	cmd.ExtraFiles = files
	cmd.WaitDelay = 2 * time.Second
	var output boundedProbeOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("probing opened media: %w", err)
	}
	var parsed FFProbeJSON
	if err := json.Unmarshal(output.buffer.Bytes(), &parsed); err != nil {
		return nil, fmt.Errorf("decoding opened media probe: %w", err)
	}
	return parseWithSize(logicalPath, &parsed, info.Size())
}
