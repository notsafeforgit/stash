package manager

import (
	"bytes"
	"context"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSceneFrameImageDoesNotPublishSceneArtwork(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required for scene frame extraction")
	}
	mgr, db := coverTestManager(t)
	mgr.FFMpeg = ffmpeg.NewEncoder(ffmpegPath)
	source := filepath.Join(t.TempDir(), "source.mkv")
	out, err := exec.Command(ffmpegPath, "-v", "error",
		"-f", "lavfi", "-i", "color=red:size=64x48:rate=8:duration=1",
		"-f", "lavfi", "-i", "color=blue:size=64x48:rate=8:duration=1",
		"-filter_complex", "[0:v][1:v]concat=n=2:v=1:a=0", "-c:v", "ffv1", source).CombinedOutput()
	require.NoError(t, err, "%s", out)
	file := &models.VideoFile{BaseFile: &models.BaseFile{Path: source}, Width: 64, Height: 48, Duration: 2}
	scene := &models.Scene{ID: 1, Path: source, CoverChecksum: "cover"}
	saveCoverTestMetadata(t, mgr, scene, 0.5)
	before := mgr.ScenePreviewImage(scene)
	sourceBefore, err := os.ReadFile(source)
	require.NoError(t, err)

	for _, at := range []float64{0, 1.5} {
		data, err := mgr.SceneFrameImage(context.Background(), file, at)
		require.NoError(t, err)
		frame, err := jpeg.Decode(bytes.NewReader(data))
		require.NoError(t, err)
		require.Equal(t, 64, frame.Bounds().Dx())
		r, _, b, _ := frame.At(32, 24).RGBA()
		if at == 0 {
			require.Greater(t, r, b, "frame zero should be red")
		} else {
			require.Greater(t, b, r, "later frame should be blue")
		}
	}
	require.Equal(t, before, mgr.ScenePreviewImage(scene))
	sourceAfter, err := os.ReadFile(source)
	require.NoError(t, err)
	require.Equal(t, sourceBefore, sourceAfter)
	db.AssertExpectations(t)
}

func TestSceneFrameImageRejectsTimeOutsideVideo(t *testing.T) {
	mgr := &Manager{}
	file := &models.VideoFile{Duration: 2}
	for _, at := range []float64{-1, 2, 3, math.NaN(), math.Inf(1)} {
		_, err := mgr.SceneFrameImage(context.Background(), file, at)
		require.ErrorContains(t, err, "within the video duration")
	}
}
