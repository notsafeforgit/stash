package manager

import (
	"fmt"
	"net/url"
	"sync"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
)

type SceneStreamEndpoint struct {
	URL      string  `json:"url"`
	MimeType *string `json:"mime_type"`
	Label    *string `json:"label"`
}

type endpointType struct {
	label     string
	mimeType  string
	extension string
}

var (
	directEndpointType = endpointType{
		label:     "Direct stream",
		mimeType:  ffmpeg.MimeMp4Video,
		extension: "",
	}
	mp4EndpointType = endpointType{
		label:     "MP4",
		mimeType:  ffmpeg.MimeMp4Video,
		extension: ".mp4",
	}
	mkvEndpointType = endpointType{
		label: "MKV",
		// use mp4 mimetype to trick the client, since many clients won't try mkv
		mimeType:  ffmpeg.MimeMp4Video,
		extension: ".mkv",
	}
	webmEndpointType = endpointType{
		label:     "WEBM",
		mimeType:  ffmpeg.MimeWebmVideo,
		extension: ".webm",
	}
	legacyHLSEndpointType = endpointType{
		label:     "HLS",
		mimeType:  ffmpeg.MimeHLS,
		extension: ".m3u8",
	}
	dashEndpointType = endpointType{
		label:     "DASH",
		mimeType:  ffmpeg.MimeDASH,
		extension: ".mpd",
	}
	// HLS endpoints point at the master playlists (`.master.m3u8`)
	// rather than the media playlists. The master is the spec-correct
	// HLS entry point and is required by MSE-based clients (e.g.
	// @videojs/spf); Safari's native HLS handles it transparently.
	hlsEndpointType = endpointType{
		label:     "HLS",
		mimeType:  ffmpeg.MimeHLS,
		extension: ".master.m3u8",
	}
	// fmp4CopyHLSEndpointType — remux the source's existing video & audio
	// streams into fragmented-MP4 HLS segments without re-encoding. The
	// fast path when the source's codecs are already browser-decodable
	// (AV1/HEVC/H.264 + Opus/AAC). The plain `hlsEndpointType` does a
	// full H.264 + AAC re-encode and is the fallback for everything else.
	fmp4CopyHLSEndpointType = endpointType{
		label:     "HLS (remux)",
		mimeType:  ffmpeg.MimeHLS,
		extension: ".fmp4.master.m3u8",
	}
	// fmp4CopyVideoAacHLSEndpointType — copy the source video bitstream
	// untouched, re-encode the audio track to AAC. Bridges the gap when
	// the browser takes the source video in fMP4 but rejects the source
	// audio (notably iOS Safari rejecting Opus-in-MP4 from
	// ManagedMediaSource). Cheaper than the full transcode (no video
	// encode) and preserves native AV1/HEVC hardware decode on the
	// client.
	fmp4CopyVideoAacHLSEndpointType = endpointType{
		label:     "HLS (remux + AAC)",
		mimeType:  ffmpeg.MimeHLS,
		extension: ".fmp4.aac.master.m3u8",
	}
)

func GetVideoFileContainer(file *models.VideoFile) (ffmpeg.Container, error) {
	var container ffmpeg.Container
	format := file.Format
	if format != "" {
		container = ffmpeg.Container(format)
	} else { // container isn't in the DB
		// shouldn't happen, fallback to ffprobe
		ffprobe := GetInstance().FFProbe
		tmpVideoFile, err := ffprobe.NewVideoFile(file.Path)
		if err != nil {
			return ffmpeg.Container(""), fmt.Errorf("error reading video file: %v", err)
		}

		return ffmpeg.MatchContainer(tmpVideoFile.Container, file.Path)
	}

	return container, nil
}

// GetV3SceneStreamPaths returns the direct and segmented stream catalog used
// exclusively by the v3 player.
func GetV3SceneStreamPaths(scene *models.Scene, directStreamURL *url.URL, maxStreamingTranscodeSize models.StreamingResolutionEnum) ([]*SceneStreamEndpoint, error) {
	if scene == nil {
		return nil, fmt.Errorf("nil scene")
	}

	pf := scene.Files.Primary()
	if pf == nil {
		return nil, nil
	}

	// convert StreamingResolutionEnum to ResolutionEnum
	maxStreamingResolution := models.ResolutionEnum(maxStreamingTranscodeSize)
	sceneResolution := models.GetMinResolution(pf)
	includeSceneStreamPath := func(streamingResolution models.StreamingResolutionEnum) bool {
		var minResolution int
		if streamingResolution == models.StreamingResolutionEnumOriginal {
			minResolution = sceneResolution
		} else {
			// convert StreamingResolutionEnum to ResolutionEnum so we can get the min
			// resolution
			convertedRes := models.ResolutionEnum(streamingResolution)
			minResolution = convertedRes.GetMinResolution()

			// don't include if scene resolution is smaller than the streamingResolution
			if sceneResolution != 0 && sceneResolution < minResolution {
				return false
			}
		}

		// if we always allow everything, then return true
		if maxStreamingTranscodeSize == models.StreamingResolutionEnumOriginal {
			return true
		}

		return maxStreamingResolution.GetMinResolution() >= minResolution
	}

	makeStreamEndpoint := func(t endpointType, resolution models.StreamingResolutionEnum) *SceneStreamEndpoint {
		url := *directStreamURL
		url.Path += t.extension

		label := t.label

		if resolution != "" {
			v := url.Query()
			v.Set("resolution", resolution.String())
			url.RawQuery = v.Encode()

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
			URL:      url.String(),
			MimeType: &t.mimeType,
			Label:    &label,
		}
	}

	var endpoints []*SceneStreamEndpoint

	// direct stream should only apply when the audio codec is supported
	audioCodec := ffmpeg.MissingUnsupported
	if pf.AudioCodec != "" {
		audioCodec = ffmpeg.ProbeAudioCodec(pf.AudioCodec)
	}

	// don't care if we can't get the container
	container, _ := GetVideoFileContainer(pf)

	hasTranscode := HasTranscode(scene, config.GetInstance().GetVideoFileNamingAlgorithm())
	if hasTranscode || ffmpeg.IsValidAudioForContainer(audioCodec, container) {
		actualDirectType := directEndpointType
		if !hasTranscode {
			// v3 uses the real source MIME for direct streams. The legacy
			// catalog retains the upstream MP4 MIME shim for compatibility.
			switch container {
			case ffmpeg.Matroska:
				actualDirectType.mimeType = ffmpeg.MimeMkvVideo
			case ffmpeg.Webm:
				actualDirectType.mimeType = ffmpeg.MimeWebmVideo
			}
		}
		endpoints = append(endpoints, makeStreamEndpoint(actualDirectType, ""))
	}

	// Codec-copy HLS variants are only viable when the source's GOPs are
	// short enough to align with our 2 s declared segment length and the
	// source does not rely on a display transform. Browsers honor MOV/MP4
	// display matrices during direct playback, and ffmpeg applies them when
	// transcoding, but MSE playback of codec-copy fMP4 can ignore the matrix
	// and display the coded pixels sideways or upside down.
	// `-c copy` HLS muxer can only end segments at source keyframes, so
	// a source whose GOPs are 5+ s ends up writing 5+ s segments while
	// our hand-rolled playlist still declares EXTINF=2.002 — hls.js loses
	// timeline coherence and the user sees judder. The transcode path
	// forces 2 s GOPs via `-g`/`-keyint_min` and is unaffected.
	//
	// Probe is bounded to the first 30 s of the source and cached, so
	// the cost is one ffprobe invocation per source per server lifetime.
	// Threshold 2.5 s leaves a small slack for "approximately 2 s" CFR
	// sources whose IDRs may overshoot by a frame or two; iPhone HDR
	// (5+ s GOPs) and most consumer-cam content (~1 s GOPs) fall
	// cleanly on either side.
	canCodecCopyHLS := !hasTranscode &&
		isFMP4VideoRemuxCandidate(pf.VideoCodec) &&
		!hasDisplayRotation(pf) &&
		hasShortHLSGops(pf)

	// fMP4-copy HLS endpoint. Offered when the source's video+audio codecs
	// can be carried in a fragmented-MP4 container without re-encoding.
	// Faster to start and cheaper on CPU than the full H.264 transcode at
	// the plain `.m3u8` endpoint. Skipped when a pre-generated transcode
	// exists or when source GOPs would break playlist alignment.
	if canCodecCopyHLS && isFMP4RemuxCandidate(pf.VideoCodec, audioCodec) {
		endpoints = append(endpoints, makeStreamEndpoint(fmp4CopyHLSEndpointType, ""))
	}

	// fMP4 video-copy + AAC-transcode HLS endpoint. Same idea as the
	// remux above but the audio track is always re-encoded to AAC. Gated
	// on the source video codec being plausibly browser-decodable in
	// fMP4 — the audio codec is irrelevant here because we're producing
	// AAC regardless. Always offered alongside the full-copy variant
	// when applicable; the client picks based on its own MSE/MMS audio
	// codec probe.
	if canCodecCopyHLS && isFMP4VideoRemuxCandidate(pf.VideoCodec) {
		endpoints = append(endpoints, makeStreamEndpoint(fmp4CopyVideoAacHLSEndpointType, ""))
	}

	// HLS transcode endpoint(s) — fallback when the source's codecs aren't
	// playable in the target browser. Per-resolution variants via
	// `?resolution=…`. The plain `.m3u8` re-encodes to H.264 + AAC fMP4.
	for _, res := range []models.StreamingResolutionEnum{
		models.StreamingResolutionEnumOriginal,
		models.StreamingResolutionEnumFourK,
		models.StreamingResolutionEnumFullHd,
		models.StreamingResolutionEnumStandardHd,
		models.StreamingResolutionEnumStandard,
		models.StreamingResolutionEnumLow,
	} {
		if includeSceneStreamPath(res) {
			endpoints = append(endpoints, makeStreamEndpoint(hlsEndpointType, res))
		}
	}

	return endpoints, nil
}

// isFMP4RemuxCandidate reports whether the source's video & audio codecs
// can be placed into a fragmented-MP4 container as-is (no re-encode). This
// is the pre-filter for the `hls-copy-fmp4` endpoint; the client still
// runs a per-browser `canPlayType` probe before picking it.
//
// Video codecs that ride in fMP4 and are plausibly browser-decodable:
// H.264, HEVC/H.265, AV1. (VP9 can be carried in fMP4 but Safari never
// decodes VP9, and Chrome/Firefox play VP9 natively from WebM/MKV already
// so remux buys us nothing — excluded.)
//
// Audio codecs that ride in fMP4 and are broadly decodable: AAC, Opus, MP3.
// A missing/unsupported audio codec is still a candidate — ffmpeg drops
// audio via `-an` in the copy-fmp4 path.
func isFMP4RemuxCandidate(videoCodec string, audioCodec ffmpeg.ProbeAudioCodec) bool {
	if !isFMP4VideoRemuxCandidate(videoCodec) {
		return false
	}
	switch audioCodec {
	case ffmpeg.Aac, ffmpeg.Opus, ffmpeg.Mp3, ffmpeg.MissingUnsupported:
		return true
	}
	return false
}

// isFMP4VideoRemuxCandidate is the audio-agnostic half of
// isFMP4RemuxCandidate — true when the source video codec rides in fMP4
// and is plausibly browser-decodable. Used by the video-copy + AAC-
// transcode HLS variant, which always produces AAC audio so the source's
// own audio codec doesn't constrain the decision.
func isFMP4VideoRemuxCandidate(videoCodec string) bool {
	switch videoCodec {
	case ffmpeg.H264, ffmpeg.H265, ffmpeg.Hevc, "av1":
		return true
	}
	return false
}

// hlsCopyMaxGOPSeconds is the largest source GOP we'll allow on the
// codec-copy HLS variants. The transcode path forces 2 s GOPs via
// `-g`/`-keyint_min`; copy mode can't insert keyframes, so the actual
// segment cadence is dictated by the source. 2.5 s leaves a frame or
// two of slack for "approximately 2 s" CFR sources whose IDR placement
// overshoots — but stays well below the iPhone-HDR-style ~5 s GOPs
// that misalign the playlist.
const hlsCopyMaxGOPSeconds = 2.5

// gopProbeWindowSeconds bounds the read interval for `MaxGOPSeconds`.
// Large enough to capture multiple keyframes on any normal source
// (even ~5 s GOPs surface 5–6 keyframes inside the window) while
// staying cheap on multi-GB files.
const gopProbeWindowSeconds = 30

// gopProbeCache memoises `hasShortHLSGops` results — `GetSceneStreamPaths`
// runs on every scene-detail page load and we don't want to re-probe a
// source for every refresh. Keyed by `path|modtime` so a re-encoded file
// at the same path invalidates automatically. sync.Map is fine here
// because the workload is read-mostly with rare writes.
var gopProbeCache sync.Map // map[string]bool

// displayRotationProbeCache memoises whether a source carries a non-zero
// display transform. Like the GOP cache, modtime is part of the key so a
// replacement file is re-probed automatically.
var displayRotationProbeCache sync.Map // map[string]bool

// hasShortHLSGops reports whether the source's GOP cadence is short
// enough that `-c copy` HLS will produce segments close to our declared
// 2 s length. False for sources with longer GOPs (notably iPhone HDR
// AV1 with 5 s GOPs); those are forced through the H.264 transcode
// instead, where we control the segment length directly.
//
// First call per file pays one ffprobe (bounded to the first 30 s of
// the file); subsequent calls are a sync.Map lookup. Probe failures
// are treated as "GOP too long" and cached as `false` — better to
// fall back to transcode than to risk serving a misaligned playlist.
func hasShortHLSGops(pf *models.VideoFile) bool {
	if pf == nil || pf.Path == "" {
		return false
	}
	key := fmt.Sprintf("%s|%d", pf.Path, pf.ModTime.Unix())
	if cached, ok := gopProbeCache.Load(key); ok {
		return cached.(bool)
	}
	probe := GetInstance().FFProbe
	maxGOP, err := probe.MaxGOPSeconds(pf.Path, gopProbeWindowSeconds)
	if err != nil {
		gopProbeCache.Store(key, false)
		return false
	}
	ok := maxGOP > 0 && maxGOP <= hlsCopyMaxGOPSeconds
	gopProbeCache.Store(key, ok)
	return ok
}

// hasDisplayRotation reports whether the source relies on rotation metadata
// to present its coded pixels correctly. Codec-copy fMP4/HLS is unsafe for
// these files because MSE clients can discard the display matrix. A probe
// failure conservatively returns true so the source uses the full transcode,
// whose decoder applies the transform before encoding.
func hasDisplayRotation(pf *models.VideoFile) bool {
	if pf == nil || pf.Path == "" {
		return true
	}
	key := fmt.Sprintf("%s|%d", pf.Path, pf.ModTime.Unix())
	if cached, ok := displayRotationProbeCache.Load(key); ok {
		return cached.(bool)
	}

	probeResult, err := GetInstance().FFProbe.NewVideoFile(pf.Path)
	if err != nil {
		displayRotationProbeCache.Store(key, true)
		return true
	}

	rotated := probeResult.Rotation != 0
	displayRotationProbeCache.Store(key, rotated)
	return rotated
}

// HasTranscode returns true if a transcoded video exists for the provided
// scene. It will check using the OSHash of the scene first, then fall back
// to the checksum.
func HasTranscode(scene *models.Scene, fileNamingAlgo models.HashAlgorithm) bool {
	if scene == nil {
		return false
	}

	sceneHash := scene.GetHash(fileNamingAlgo)
	if sceneHash == "" {
		return false
	}

	transcodePath := instance.Paths.Scene.GetTranscodePath(sceneHash)
	ret, _ := fsutil.FileExists(transcodePath)
	return ret
}
