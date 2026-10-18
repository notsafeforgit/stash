package manager

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scene/generate"
	"github.com/stretchr/testify/require"
)

type cancellationPaths struct {
	generate.ScenePaths
	dir string
}

func (p cancellationPaths) TempFile(pattern string) (*os.File, error) {
	return os.CreateTemp(p.dir, pattern)
}
func (p cancellationPaths) GetVideoPreviewPath(string) string {
	return filepath.Join(p.dir, "preview.mp4")
}

func TestMediaCancellationStopsProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("synthetic POSIX media process")
	}
	config.InitializeEmpty()
	for _, kind := range []string{"preview", "probe", "frame count", "sprite"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			started := filepath.Join(dir, "started")
			t.Setenv("STASH_TEST_MEDIA_STARTED", started)
			binary := filepath.Join(dir, "media-process")
			require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\nif [ \"$1\" = \"-version\" ]; then echo 'ffmpeg version 7.1.0'; exit 0; fi\nprintf 'started\\n' >> \"$STASH_TEST_MEDIA_STARTED\"\nexec sleep 30\n"), 0700))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			g := &generate.Generator{Encoder: ffmpeg.NewEncoder(binary), FFMpegConfig: config.GetInstance(),
				LockManager: fsutil.NewReadLockManager(), ScenePaths: cancellationPaths{dir: dir}}
			probe := ffmpeg.NewFFProbe(binary)
			result := make(chan error, 1)
			go func() {
				var err error
				switch kind {
				case "preview":
					task := GeneratePreviewTask{Scene: models.Scene{Path: "fixture.mp4"}, generator: g,
						Options: generate.PreviewOptions{Segments: 1, SegmentDuration: 5}}
					err = task.generateVideo(ctx, "fixture", 4, 30)
				case "probe":
					_, err = probe.NewVideoFileContext(ctx, "fixture.mp4")
				case "frame count":
					_, err = probe.GetReadFrameCountContext(ctx, "fixture.mp4")
				case "sprite":
					sprite := SpriteGenerator{g: g, Info: &generatorInfo{ChunkCount: 1, VideoFile: ffmpeg.VideoFile{Path: "fixture.mp4", VideoStreamDuration: 4}},
						Config: DefaultSpriteGeneratorConfig, ImageOutputPath: filepath.Join(dir, "sprite.jpg")}
					err = sprite.Generate(ctx)
				}
				result <- err
			}()
			require.Eventually(t, func() bool { _, err := os.Stat(started); return err == nil }, time.Second, time.Millisecond)
			cancel()
			select {
			case err := <-result:
				require.Error(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("media process outlived cancellation")
			}
			data, err := os.ReadFile(started)
			require.NoError(t, err)
			require.Equal(t, "started\n", string(data), "cancellation must not launch fallback processes")
			require.NoFileExists(t, filepath.Join(dir, "preview.mp4"))
			require.NoFileExists(t, filepath.Join(dir, "sprite.jpg"))
		})
	}
}
