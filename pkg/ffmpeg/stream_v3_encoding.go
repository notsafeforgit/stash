package ffmpeg

import (
	"fmt"
	"math"
)

// Live HLS needs a bounded delivery rate. Quality-only hardware encoding can
// turn a 4K source into 60+ Mbps segments, exhausting a phone's network/buffer
// immediately after a seek. Keep resolution and frame rate; constrain the
// encoder's rate instead. These are peak video rates, independent of the
// original codec's (often much lower AV1/HEVC) average bitrate.
type hlsEncoding struct {
	level        int
	videoBitrate int
}

func liveHLSEncoding(width, height int, frameRate float64) hlsEncoding {
	if frameRate <= 0 || math.IsNaN(frameRate) || math.IsInf(frameRate, 0) {
		frameRate = 30
	}
	pixels := float64(width) * float64(height)
	bitrate := 24_000_000
	switch {
	case pixels <= 0:
		bitrate = 8_000_000
	case pixels <= 426*240:
		bitrate = 1_000_000
	case pixels <= 854*480:
		bitrate = 2_500_000
	case pixels <= 1280*720:
		bitrate = 4_500_000
	case pixels <= 1920*1080:
		bitrate = 8_000_000
	case pixels <= 2560*1440:
		bitrate = 14_000_000
	}
	if frameRate > 30 {
		bitrate = bitrate * 3 / 2
	}

	// H.264 Annex A macroblock limits. In particular, UHD at 60 fps needs
	// Level 5.2, not 5.1. Use the same level in the encoder and CODECS so
	// Safari doesn't create a decoder for an understated stream capability.
	blockWidth, blockHeight := math.Ceil(float64(width)/16), math.Ceil(float64(height)/16)
	blocks := blockWidth * blockHeight
	level := 52
	for _, limit := range []struct {
		level, frameBlocks, blocksPerSecond int
	}{
		{41, 8192, 245760},
		{42, 8704, 522240},
		{51, 36864, 983040},
		{52, 36864, 2073600},
		{60, 139264, 4177920},
		{61, 139264, 8355840},
		{62, 139264, 16711680},
	} {
		level = limit.level
		maxBlocks := float64(limit.frameBlocks)
		if blocks <= maxBlocks && blocks*frameRate <= float64(limit.blocksPerSecond) &&
			blockWidth*blockWidth <= 8*maxBlocks && blockHeight*blockHeight <= 8*maxBlocks {
			break
		}
	}
	return hlsEncoding{level: level, videoBitrate: bitrate}
}

func (e hlsEncoding) codecString() string {
	return fmt.Sprintf("avc1.6400%02x", e.level)
}

func (e hlsEncoding) bandwidth(audio bool) int {
	// A full one-second VBV buffer can burst into a two-second segment.
	// Advertise that upper bound plus container overhead and AAC, not the
	// average target rate. The rendition is produced on demand, so its exact
	// peak cannot be measured before serving the master playlist.
	ret := e.videoBitrate * 3 / 2 * 105 / 100
	if audio {
		ret += 128_000
	}
	return ret
}

func (e hlsEncoding) args(codec VideoCodec) Args {
	profile, level := "high", fmt.Sprintf("%d.%d", e.level/10, e.level%10)
	if codec == VideoCodecR264 {
		// V4L2 uses AVCodecContext's numeric IDs; unlike x264/QSV/VAAPI,
		// it does not register the named H.264 profile and level options.
		profile, level = "100", fmt.Sprint(e.level)
	}
	args := Args{
		"-profile:v", profile, "-level:v", level,
		"-b:v", fmt.Sprint(e.videoBitrate * 3 / 4),
		"-maxrate:v", fmt.Sprint(e.videoBitrate),
		"-bufsize:v", fmt.Sprint(e.videoBitrate),
		"-b:a", "128k",
	}
	switch codec {
	case VideoCodecI264C:
		// The compatibility preset's -q selects CQP, which ignores the
		// bitrate ceiling. VBR works on the older QSV implementations too.
		args = append(args, "-q:v", "0", "-flags:v", "+cgop-qscale")
	case VideoCodecV264:
		// An explicit VBR mode prevents the preset's -qp selecting CQP.
		args = append(args, "-rc_mode", "VBR")
	}
	return args
}
