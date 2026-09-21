package ffmpeg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPrivateV3StreamsStripSourceMetadata(t *testing.T) {
	encoder, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required for the private rendition regression")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mp4")
	output, err := exec.CommandContext(ctx, encoder, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=30", "-t", "3", "-c:v", "libx264", "-g", "60", "-metadata", "title=PRIVATE_SOURCE_TITLE", "-metadata", "comment=PRIVATE_COMMENT", "-metadata:s:v", "title=PRIVATE_TRACK_TITLE", source).CombinedOutput()
	require.NoError(t, err, string(output))
	for _, streamType := range []*V3StreamType{V3StreamTypeHLS, V3StreamTypeHLSCopyFMP4, V3StreamTypeHLSCopyFMP4AAC} {
		t.Run(streamType.Name, func(t *testing.T) {
			out := t.TempDir()
			codec := VideoCodecCopy
			if streamType == V3StreamTypeHLS {
				codec = VideoCodecLibX264
			}
			args := Args{"-hide_banner", "-loglevel", "error", "-i", source}
			args = append(args, privateV3MetadataArgs()...)
			args = append(args, streamType.Args(codec, 0, "", true, out, 30)...)
			output, err := exec.CommandContext(ctx, encoder, args...).CombinedOutput()
			require.NoError(t, err, string(output))
			initialization, err := os.ReadFile(filepath.Join(out, ".init_video.mp4"))
			require.NoError(t, err)
			for _, secret := range []string{"PRIVATE_SOURCE_TITLE", "PRIVATE_COMMENT", "PRIVATE_TRACK_TITLE"} {
				require.NotContains(t, string(initialization), secret)
			}
		})
	}
}
