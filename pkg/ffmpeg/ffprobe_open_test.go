package ffmpeg

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func openedTestProbe(t *testing.T) *FFProbe {
	t.Helper()
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	return NewFFProbe(path)
}

func TestOpenedProbeUsesDescriptorAfterPathReplacement(t *testing.T) {
	probe := openedTestProbe(t)
	path := filepath.Join(t.TempDir(), "original.png")
	var content bytes.Buffer
	require.NoError(t, png.Encode(&content, image.NewRGBA(image.Rect(0, 0, 17, 23))))
	require.NoError(t, os.WriteFile(path, content.Bytes(), 0600))
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, os.Rename(path, path+"-old"))
	require.NoError(t, os.WriteFile(path, []byte("not an image"), 0600))
	result, err := probe.NewVideoFileFromOpen(t.Context(), f, path)
	require.NoError(t, err)
	require.Equal(t, path, result.Path)
	require.Equal(t, int64(content.Len()), result.Size)
	require.Equal(t, 17, result.Width)
	require.Equal(t, 23, result.Height)
	require.Equal(t, "png", result.VideoCodec)
	// The caller's descriptor remains open with its original read position.
	position, err := f.Seek(0, io.SeekCurrent)
	require.NoError(t, err)
	require.Zero(t, position)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = probe.NewVideoFileFromOpen(ctx, f, path)
	require.ErrorIs(t, err, context.Canceled)
}

func TestOpenedProbeSeeksToTrailingMP4Metadata(t *testing.T) {
	probe := openedTestProbe(t)
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	path := filepath.Join(t.TempDir(), "trailing-moov.mp4")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, ffmpegPath, "-v", "error", "-f", "lavfi", "-i", "color=size=32x48:rate=2", "-t", "1", "-c:v", "mpeg4", "-threads", "1", "-y", path).CombinedOutput()
	require.NoError(t, err, string(output))
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Greater(t, bytes.Index(body, []byte("moov")), bytes.Index(body, []byte("mdat")))
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, os.Rename(path, path+"-moved"))
	result, err := probe.NewVideoFileFromOpen(ctx, f, path)
	require.NoError(t, err)
	require.Equal(t, 32, result.Width)
	require.Equal(t, 48, result.Height)
	require.Equal(t, "mpeg4", result.VideoCodec)
	require.InDelta(t, 1, result.FileDuration, 0.1)
}

func TestOpenedProbeRejectsIndirectSources(t *testing.T) {
	probe := openedTestProbe(t)
	for name, body := range map[string]string{
		"playlist.m3u8": "#EXTM3U\n#EXT-X-TARGETDURATION:5\n#EXTINF:5,\nhttp://127.0.0.1:1/segment.ts\n#EXT-X-ENDLIST\n",
		"concat.txt":    "ffconcat version 1.0\nfile '/etc/passwd'\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			require.NoError(t, os.WriteFile(path, []byte(body), 0600))
			f, err := os.Open(path)
			require.NoError(t, err)
			defer f.Close()
			_, err = probe.NewVideoFileFromOpen(t.Context(), f, path)
			require.Error(t, err)
		})
	}
}

func TestOpenedProbeOutputLimitCannotBeBypassedByCopy(t *testing.T) {
	var output boundedProbeOutput
	// Hide WriterTo, as the os/exec pipe does. Embedding bytes.Buffer here would
	// expose ReadFrom and accidentally bypass the writer's size limit.
	source := struct{ io.Reader }{strings.NewReader(strings.Repeat("x", maxOpenedProbeJSON+1))}
	_, err := io.Copy(&output, source)
	require.ErrorContains(t, err, "exceeds 1 MiB")
	require.LessOrEqual(t, output.buffer.Len(), maxOpenedProbeJSON)
}
