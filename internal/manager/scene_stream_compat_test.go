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

func TestSceneStreamCatalogsRemainIsolated(t *testing.T) {
	cfg := config.InitializeEmpty()
	cfg.SetBool(config.EnableV3UI, true)
	require.True(t, cfg.GetEnableV3UI())

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

	legacy, err := GetSceneStreamPaths(scene, streamURL, models.StreamingResolutionEnumOriginal)
	require.NoError(t, err)
	require.Len(t, legacy, 21)

	assert.Equal(t, "Direct stream", *legacy[0].Label)
	assert.Equal(t, ffmpeg.MimeMp4Video, *legacy[0].MimeType, "legacy clients rely on the upstream direct-stream MIME shim")
	assert.Contains(t, endpointPaths(legacy), "/scene/42/stream.mp4")
	assert.Contains(t, endpointPaths(legacy), "/scene/42/stream.webm")
	assert.Contains(t, endpointPaths(legacy), "/scene/42/stream.m3u8")
	assert.Contains(t, endpointPaths(legacy), "/scene/42/stream.mpd")
	for _, endpoint := range legacy {
		assert.NotContains(t, endpoint.URL, ".master.m3u8")
	}

	v3, err := GetV3SceneStreamPaths(scene, streamURL, models.StreamingResolutionEnumOriginal)
	require.NoError(t, err)
	require.Len(t, v3, 6)

	assert.Equal(t, ffmpeg.MimeWebmVideo, *v3[0].MimeType)
	for _, endpoint := range v3[1:] {
		assert.True(t, strings.HasSuffix(mustParseURL(t, endpoint.URL).Path, "/stream.master.m3u8"))
	}
	for _, endpoint := range v3 {
		path := mustParseURL(t, endpoint.URL).Path
		assert.NotEqual(t, "/scene/42/stream.mp4", path)
		assert.NotEqual(t, "/scene/42/stream.webm", path)
		assert.NotEqual(t, "/scene/42/stream.m3u8", path)
		assert.NotEqual(t, "/scene/42/stream.mpd", path)
	}
}

func endpointPaths(endpoints []*SceneStreamEndpoint) []string {
	ret := make([]string, len(endpoints))
	for i, endpoint := range endpoints {
		u, err := url.Parse(endpoint.URL)
		if err != nil {
			ret[i] = endpoint.URL
			continue
		}
		ret[i] = u.Path
	}
	return ret
}

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	return u
}
