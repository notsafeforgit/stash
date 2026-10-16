package ffmpeg

import (
	"bytes"
	"fmt"
	"math"
	"net/http"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/utils"
)

// HDR needs a 10-bit codec throughout the decode/scale/encode path. The
// H.264 endpoint remains the tone-mapped SDR compatibility rendition.
var V3StreamTypeHLSHEVC = &V3StreamType{
	Name:        "hls-hevc",
	SegmentType: V3SegmentTypeFMP4,
	Args:        V3StreamTypeHLS.Args,
}

// Offer live HDR only with a hardware HEVC encoder and scanned HDR metadata.
// Software HEVC remains usable for explicit requests and regression fixtures,
// but must not be an automatic 4K live-playback choice.
func (sm *StreamManager) CanStreamHDR(vf *models.VideoFile) bool {
	return sm != nil && sm.encoder != nil && sm.config != nil && vf != nil &&
		colorMetadataFromModel(vf).isHDR() && sm.config.GetTranscodeHardwareAcceleration() &&
		sm.encoder.hwCodecHDRHLSCompatible() != nil
}

func (f *FFMpeg) hwCodecHDRHLSCompatible() *VideoCodec {
	for _, codec := range f.getHWCodecSupport() {
		if codec == VideoCodecV265HDR {
			return &codec
		}
	}
	return nil
}

type hevcHLSEncoding struct {
	level        int // general_level_idc, thirty times the HEVC level
	videoBitrate int
}

func liveHEVCHLSEncoding(width, height int, frameRate float64) hevcHLSEncoding {
	if frameRate <= 0 || math.IsNaN(frameRate) || math.IsInf(frameRate, 0) {
		frameRate = 30
	}
	pixels := float64(width) * float64(height)
	level := 186
	// HEVC Annex A Main-tier luma picture/sample-rate limits. 4K60 needs
	// level 5.1, and portrait video has the same sample count as landscape.
	for _, limit := range []struct {
		level         int
		picture, rate float64
	}{
		{90, 552960, 16588800}, {93, 983040, 33177600},
		{120, 2228224, 66846720}, {123, 2228224, 133693440},
		{150, 8912896, 267386880}, {153, 8912896, 534773760},
		{156, 8912896, 1069547520}, {180, 35651584, 1069547520},
		{183, 35651584, 2139095040}, {186, 35651584, 4278190080},
	} {
		if pixels <= float64(limit.picture) && pixels*frameRate <= float64(limit.rate) &&
			float64(width)*float64(width) <= 8*float64(limit.picture) &&
			float64(height)*float64(height) <= 8*float64(limit.picture) {
			level = limit.level
			break
		}
	}
	return hevcHLSEncoding{level: level, videoBitrate: liveHLSEncoding(width, height, frameRate).videoBitrate * 2 / 3}
}

func (e hevcHLSEncoding) codecString() string {
	return fmt.Sprintf("hvc1.2.4.L%d.B0", e.level)
}

func (e hevcHLSEncoding) args(codec VideoCodec) Args {
	args := Args{
		"-profile:v", "main10", "-tag:v", "hvc1",
		"-b:v", fmt.Sprint(e.videoBitrate * 3 / 4),
		"-maxrate:v", fmt.Sprint(e.videoBitrate), "-bufsize:v", fmt.Sprint(e.videoBitrate),
		"-b:a", "128k",
	}
	if codec == VideoCodecLibX265 {
		args = append(args, "-pix_fmt", "yuv420p10le", "-preset", "veryfast",
			"-x265-params", fmt.Sprintf("level-idc=%g:high-tier=0:open-gop=0:scenecut=0:repeat-headers=0", float64(e.level)/30))
	} else {
		// VAAPI uses AVCodecContext's numeric level ID. VBR, unlike the
		// download endpoint's CQP, respects the live stream's bandwidth.
		args = append(args, "-level:v", fmt.Sprint(e.level), "-rc_mode", "VBR")
	}
	return args
}

func hlsHEVCFilter(codec VideoCodec, vf *models.VideoFile, maxSize int, fullhw bool, color videoColorMetadata) VideoFilter {
	var input VideoFilter
	if color.isHDR() {
		input = hdrInputFilter(color)
	}
	if codec == VideoCodecLibX265 {
		return input.ScaleMax(vf.Width, vf.Height, maxSize).Append("format=yuv420p10le")
	}
	return prependVideoFilter(input, hevcHWVideoFilter(codec, vf, maxSize, fullhw, color))
}

func serveV3HLSMasterManifestHEVC(sm *StreamManager, w http.ResponseWriter, r *http.Request, vf *models.VideoFile, resolution string) {
	maxResolution := 0
	if resolution != "" {
		maxResolution = models.StreamingResolutionEnum(resolution).GetMaxResolution()
	} else if sm.config != nil {
		maxResolution = sm.config.GetMaxStreamingTranscodeSize().GetMaxResolution()
	}
	width, height := scaledDimensions(vf, maxResolution)
	encoding := liveHEVCHLSEncoding(width, height, vf.FrameRateFinite())
	var probe *VideoFile
	if sm.ffprobe != nil {
		probe, _ = sm.ffprobe.NewVideoFile(vf.Path)
	}
	videoURL := hlsMasterTrackURL(r, TrackVideo)
	var audioURL string
	codecs := encoding.codecString()
	if ProbeAudioCodec(vf.AudioCodec) != MissingUnsupported {
		audioURL = hlsMasterTrackURL(r, TrackAudio)
		codecs += ",mp4a.40.2"
	}
	var buf bytes.Buffer
	writeMasterPlaylist(&buf, videoURL, audioURL,
		hlsEncoding{videoBitrate: encoding.videoBitrate}.bandwidth(audioURL != ""),
		width, height, codecs, sourceColorMetadata(vf, probe).hlsVideoRange())
	w.Header().Set("Content-Type", MimeHLS)
	utils.ServeStaticContent(w, r, buf.Bytes())
}
