package manager

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stretchr/testify/require"
)

func TestOriginalStreamCatalogDoesNotSubstituteOwnerTranscode(t *testing.T) {
	cfg := config.InitializeEmpty()
	p := paths.NewPaths(t.TempDir(), "")
	previous := instance
	instance = &Manager{Config: cfg, Paths: &p}
	t.Cleanup(func() { instance = previous })
	vf := &models.VideoFile{
		BaseFile: &models.BaseFile{Fingerprints: models.Fingerprints{
			{Type: models.FingerprintTypeMD5, Fingerprint: "original-md5"},
			{Type: models.FingerprintTypeOshash, Fingerprint: "original-oshash"},
		}},
		Format: string(ffmpeg.Webm), VideoCodec: ffmpeg.Vp9, AudioCodec: "opus", Width: 1920, Height: 1080,
	}
	scene := &models.Scene{OSHash: "original-oshash", Checksum: "original-md5", Files: models.NewRelatedVideoFiles([]*models.VideoFile{vf})}
	transcode := p.Scene.GetTranscodePath(scene.GetHash(cfg.GetVideoFileNamingAlgorithm()))
	require.NoError(t, os.MkdirAll(filepath.Dir(transcode), 0o700))
	require.NoError(t, os.WriteFile(transcode, []byte("owner MP4 transcode"), 0o600))
	base := &url.URL{Path: "/share/grant/media/scene-1/stream"}
	owner, err := GetV3SceneStreamPaths(scene, base, models.StreamingResolutionEnumFullHd)
	require.NoError(t, err)
	require.Equal(t, ffmpeg.MimeMp4Video, *owner[0].MimeType)
	original, err := GetV3OriginalSceneStreamPaths(scene, base, models.StreamingResolutionEnumFullHd)
	require.NoError(t, err)
	require.Equal(t, ffmpeg.MimeWebmVideo, *original[0].MimeType)
	require.Greater(t, len(original), 1, "original playback must retain HLS compatibility options")
	for _, endpoint := range original[1:] {
		require.Contains(t, endpoint.URL, base.Path+".master.m3u8")
	}
}
