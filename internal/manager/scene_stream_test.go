package manager

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/models"
)

func TestSceneStreamCatalogUsesNativePlayback(t *testing.T) {
	config.InitializeEmpty()

	scene := &models.Scene{
		Files: models.NewRelatedVideoFiles([]*models.VideoFile{
			{
				BaseFile:   &models.BaseFile{},
				Format:     string(ffmpeg.Webm),
				Width:      1920,
				Height:     1080,
				VideoCodec: ffmpeg.Vp9,
				AudioCodec: "opus",
			},
		}),
	}
	streamURL, err := url.Parse("https://stash.example/scene/42/stream?apikey=secret")
	require.NoError(t, err)

	streams, err := GetSceneStreamPaths(scene, streamURL, models.StreamingResolutionEnumOriginal)
	require.NoError(t, err)
	require.Len(t, streams, 6)

	assert.Equal(t, ffmpeg.MimeWebmVideo, *streams[0].MimeType)
	for _, endpoint := range streams[1:] {
		assert.True(t, strings.HasSuffix(mustParseURL(t, endpoint.URL).Path, "/stream.master.m3u8"))
	}
	for _, endpoint := range streams {
		path := mustParseURL(t, endpoint.URL).Path
		assert.NotEqual(t, "/scene/42/stream.mp4", path)
		assert.NotEqual(t, "/scene/42/stream.webm", path)
		assert.NotEqual(t, "/scene/42/stream.m3u8", path)
		assert.NotEqual(t, "/scene/42/stream.mpd", path)
	}
}

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	return u
}
