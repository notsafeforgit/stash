package manager

import (
	"fmt"
	"net/url"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/models"
)

// GetSceneStreamPaths preserves the upstream sceneStreams contract for the
// v2.5 web UI and external API clients. Keep this catalog independent from the
// v3 segmented-stream catalog so enabling v3 does not change existing API
// semantics.
func GetSceneStreamPaths(scene *models.Scene, directStreamURL *url.URL, maxStreamingTranscodeSize models.StreamingResolutionEnum) ([]*SceneStreamEndpoint, error) {
	if scene == nil {
		return nil, fmt.Errorf("nil scene")
	}

	pf := scene.Files.Primary()
	if pf == nil {
		return nil, nil
	}

	maxStreamingResolution := models.ResolutionEnum(maxStreamingTranscodeSize)
	sceneResolution := models.GetMinResolution(pf)
	includeSceneStreamPath := func(streamingResolution models.StreamingResolutionEnum) bool {
		var minResolution int
		if streamingResolution == models.StreamingResolutionEnumOriginal {
			minResolution = sceneResolution
		} else {
			convertedRes := models.ResolutionEnum(streamingResolution)
			minResolution = convertedRes.GetMinResolution()

			if sceneResolution != 0 && sceneResolution < minResolution {
				return false
			}
		}

		if maxStreamingTranscodeSize == models.StreamingResolutionEnumOriginal {
			return true
		}

		return maxStreamingResolution.GetMinResolution() >= minResolution
	}

	makeStreamEndpoint := func(t endpointType, resolution models.StreamingResolutionEnum) *SceneStreamEndpoint {
		u := *directStreamURL
		u.Path += t.extension

		label := t.label
		if resolution != "" {
			v := u.Query()
			v.Set("resolution", resolution.String())
			u.RawQuery = v.Encode()

			switch resolution {
			case models.StreamingResolutionEnumFourK:
				label += " 4K (2160p)"
			case models.StreamingResolutionEnumFullHd:
				label += " Full HD (1080p)"
			case models.StreamingResolutionEnumStandardHd:
				label += " HD (720p)"
			case models.StreamingResolutionEnumStandard:
				label += " Standard (480p)"
			case models.StreamingResolutionEnumLow:
				label += " Low (240p)"
			}
		}

		return &SceneStreamEndpoint{
			URL:      u.String(),
			MimeType: &t.mimeType,
			Label:    &label,
		}
	}

	var endpoints []*SceneStreamEndpoint

	audioCodec := ffmpeg.MissingUnsupported
	if pf.AudioCodec != "" {
		audioCodec = ffmpeg.ProbeAudioCodec(pf.AudioCodec)
	}

	container, _ := GetVideoFileContainer(pf)
	if HasTranscode(scene, config.GetInstance().GetVideoFileNamingAlgorithm()) || ffmpeg.IsValidAudioForContainer(audioCodec, container) {
		endpoints = append(endpoints, makeStreamEndpoint(directEndpointType, ""))
	}

	if container == ffmpeg.Matroska {
		endpoints = append(endpoints, makeStreamEndpoint(mkvEndpointType, ""))
	}

	var mp4Streams []*SceneStreamEndpoint
	var webmStreams []*SceneStreamEndpoint
	var hlsStreams []*SceneStreamEndpoint
	var dashStreams []*SceneStreamEndpoint

	for _, resolution := range []models.StreamingResolutionEnum{
		models.StreamingResolutionEnumOriginal,
		models.StreamingResolutionEnumFourK,
		models.StreamingResolutionEnumFullHd,
		models.StreamingResolutionEnumStandardHd,
		models.StreamingResolutionEnumStandard,
		models.StreamingResolutionEnumLow,
	} {
		if !includeSceneStreamPath(resolution) {
			continue
		}

		mp4Streams = append(mp4Streams, makeStreamEndpoint(mp4EndpointType, resolution))
		webmStreams = append(webmStreams, makeStreamEndpoint(webmEndpointType, resolution))
		hlsStreams = append(hlsStreams, makeStreamEndpoint(legacyHLSEndpointType, resolution))
		dashStreams = append(dashStreams, makeStreamEndpoint(dashEndpointType, resolution))
	}

	endpoints = append(endpoints, mp4Streams...)
	endpoints = append(endpoints, webmStreams...)
	endpoints = append(endpoints, hlsStreams...)
	endpoints = append(endpoints, dashStreams...)

	return endpoints, nil
}
