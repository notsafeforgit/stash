package ffmpeg

import (
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

// Probe tags take precedence over stored scan metadata. Older library entries
// may have no color fields; a failed probe must not discard known HDR tags.
func sourceColorMetadata(vf *models.VideoFile, probe *VideoFile) videoColorMetadata {
	color := colorMetadataFromModel(vf)
	probed := colorMetadataFromProbe(probe)
	if probed.Range != "" {
		color.Range = probed.Range
	}
	if probed.Space != "" {
		color.Space = probed.Space
	}
	if probed.Transfer != "" {
		color.Transfer = probed.Transfer
	}
	if probed.Primaries != "" {
		color.Primaries = probed.Primaries
	}
	return color
}

func (m videoColorMetadata) hlsVideoRange() string {
	switch normalizeColorTag(m.Transfer) {
	case colorTransferHLG:
		return "HLG"
	case colorTransferPQ:
		return "PQ"
	default:
		return "SDR"
	}
}

// HDR input occasionally omits the matrix/primaries/range. Restore scan/probe
// tags, using HDR defaults for missing fields. Never relabel these pixels SDR.
func hdrInputFilter(color videoColorMetadata) VideoFilter {
	transfer := colorTransferHLG
	if color.isPQ() {
		transfer = colorTransferPQ
	}
	filter := "setparams=color_trc=" + transfer
	filter += ":color_primaries=" + colorFilterToken(color.Primaries, "bt2020")
	filter += ":colorspace=" + colorFilterToken(color.Space, "bt2020nc")
	filter += ":range=" + colorFilterToken(color.Range, "limited")
	return VideoFilter(filter)
}

func colorFilterToken(value, fallback string) string {
	value = normalizeColorTag(value)
	if value == "" {
		return fallback
	}
	for _, c := range value {
		if c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' {
			continue
		}
		return fallback
	}
	return value
}

// H.264 HLS is the SDR compatibility variant. Keep HDR precision until after
// tone mapping, and only then feed the existing 8-bit scaler/encoder pipeline.
// Intel's VPP tonemapper handles HDR10, not HLG; use OpenCL for both transfers.
// The exact GPU chain is probed before use. Other devices use the software
// conversion while retaining their hardware encoder.
func (f *FFMpeg) hlsHDRFilter(codec VideoCodec, color videoColorMetadata, fullhw bool) (VideoFilter, bool) {
	filter := hdrInputFilter(color)
	if fullhw {
		var device string
		switch codec {
		case VideoCodecI264, VideoCodecI264C:
			device = "qsv"
		case VideoCodecV264:
			device = "vaapi"
		default:
			return "", false
		}
		filter = filter.Append("hwmap=derive_device=opencl")
		filter = filter.Append("tonemap_opencl=tonemap=mobius:format=nv12:p=bt709:t=bt709:m=bt709:r=tv")
		filter = filter.Append("hwmap=derive_device=" + device + ":reverse=1:extra_hw_frames=64")
		filter = filter.Append("format=" + device)
	} else {
		filter = filter.Append("zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709")
		filter = filter.Append("tonemap=mobius:param=0.3:desat=2")
		filter = filter.Append("zscale=t=bt709:m=bt709:r=limited,format=yuv420p")
	}
	// HDR side data describes the original signal, not these SDR pixels. Keep
	// unrelated side data (captions, etc.) rather than dropping everything.
	// FFmpeg before 7.1 knows these AVFrameSideDataType values from libavutil/frame.h,
	// but their sidedata filter does not expose the Dolby Vision names yet.
	// Numeric selectors remove the same data on those supported releases.
	doviRPUBuffer, doviMetadata := "DOVI_RPU_BUFFER", "DOVI_METADATA"
	if !f.version.Gteq(Version{major: 7, minor: 1}) {
		doviRPUBuffer, doviMetadata = "23", "24"
	}
	for _, kind := range []string{"MASTERING_DISPLAY_METADATA", "CONTENT_LIGHT_LEVEL", "DYNAMIC_HDR_PLUS", doviRPUBuffer, doviMetadata} {
		filter = filter.Append("sidedata=mode=delete:type=" + kind)
	}
	return filter, true
}

func hlsSDRColorArgs() Args {
	return Args{"-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709", "-color_range", "tv"}
}

// Recent ffprobe builds expose the complete RFC 6381 string, including HDR
// bit depth, transfer and primaries. Use it instead of an 8-bit guess. Only
// accept a single codec token matching this track, never playlist syntax.
func probedVideoCodecString(codec string, probe *VideoFile) string {
	if probe == nil || probe.VideoStream == nil {
		return ""
	}
	value := probe.VideoStream.MimeCodecString
	prefix := ""
	switch codec {
	case H264:
		prefix = "avc1."
	case H265, Hevc:
		// The copy muxer explicitly writes an hvc1 sample entry.
		value = strings.Replace(value, "hev1.", "hvc1.", 1)
		prefix = "hvc1."
	case "av1":
		prefix = "av01."
	case Vp9:
		prefix = "vp09."
	}
	if prefix == "" || !strings.HasPrefix(value, prefix) || len(value) > 128 {
		return ""
	}
	for _, c := range value {
		if c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			continue
		}
		return ""
	}
	return value
}
