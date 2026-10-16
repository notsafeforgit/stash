package previewimage

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"image/png"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/stashapp/stash/pkg/ffmpeg"
)

// Picker previews are deliberately transient: no store, sprite manifest, AVIF
// renditions or library writes. A request holds at most 25 small frames.
const PickerFrameCount = 25

type PickerRequest struct {
	Start  float64
	End    float64
	Frames bool
}

func (r PickerRequest) Valid(duration float64) bool {
	return !math.IsNaN(r.Start) && !math.IsNaN(r.End) && !math.IsInf(duration, 0) &&
		r.Start >= 0 && r.Start < r.End && r.End <= duration
}

type PickerFrame struct {
	// At is a seek time that reproduces this decoded frame, not a synthetic
	// frame number derived from average FPS (which is wrong for VFR sources).
	At   float64 `json:"at"`
	JPEG []byte  `json:"jpeg"`
}

func (e Encoder) Picker(ctx context.Context, video *ffmpeg.VideoFile, req PickerRequest) ([]PickerFrame, error) {
	if video == nil || !req.Valid(video.VideoStreamDuration) {
		return nil, fmt.Errorf("invalid thumbnail preview range")
	}
	if req.Frames {
		return e.pickerFrames(ctx, video, req.Start, PickerFrameCount)
	}
	var frames []PickerFrame
	// Sparse samples seek independently; never decode a whole long movie just
	// to find nine overview frames. End is exclusive, avoiding a seek at EOF.
	for i := 0; i < 9; i++ {
		at := req.Start + (req.End-req.Start)*float64(i)/9
		part, err := e.pickerFrames(ctx, video, at, 1)
		if err != nil {
			return nil, err
		}
		if len(frames) == 0 || part[0].At > frames[len(frames)-1].At {
			frames = append(frames, part[0])
		}
	}
	return frames, nil
}

// Bound both image output and diagnostic output even for malformed inputs.
type pickerBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *pickerBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, fmt.Errorf("thumbnail preview output exceeded its limit")
	}
	return b.buffer.Write(p)
}

var pickerTimeBase = regexp.MustCompile(`config in time_base: (\d+)/(\d+)`)
var pickerPTS = regexp.MustCompile(`\bn:\s*\d+\s+pts:\s*(-?\d+)`)

func (e Encoder) pickerFrames(ctx context.Context, video *ffmpeg.VideoFile, at float64, count int) ([]PickerFrame, error) {
	// FFmpeg seeks at microsecond precision. showinfo
	// runs before rescaling and reports integer PTS, avoiding pts_time's rounded
	// display value. Its PTS are relative to this input seek.
	at = math.Round(at*1e6) / 1e6
	args := ffmpeg.Args{"-hide_banner", "-loglevel", "info", "-nostdin", "-threads", "2"}.Seek(at).Input(video.Path)
	filters := []string{"showinfo=checksum=0",
		"scale=w='max(1,round(iw*sar))':h=ih:flags=bilinear", "setsar=1", boundedScale(480)}
	if IsHDR(video.ColorTransfer) {
		filters = append(filters, fmt.Sprintf("setparams=color_primaries=%s:color_trc=%s:colorspace=%s:range=%s",
			colorTag(video.ColorPrimaries, "bt2020"), video.ColorTransfer, colorTag(video.ColorSpace, "bt2020nc"), colorTag(video.ColorRange, "limited")),
			"zscale=t=linear:npl=203", "format=gbrpf32le", "zscale=p=bt709", "tonemap=mobius:param=0.3:desat=2", "zscale=t=iec61966-2-1:r=full")
	}
	// PNG framing can be decoded incrementally without guessing JPEG byte
	// delimiters. Convert only the bounded previews to JPEG for the response.
	filters = append(filters, "format=rgb24")
	args = append(args, "-map", "0:v:0", "-an", "-sn", "-filter_threads", "1", "-vf", strings.Join(filters, ","),
		"-vsync", "0", "-frames:v", strconv.Itoa(count), "-threads", "1", "-c:v", "png", "-f", "image2pipe", "pipe:1")
	output := &pickerBuffer{limit: 32 << 20}
	diagnostic := &pickerBuffer{limit: 512 << 10}
	cmd := e.FFmpeg.Command(ctx, args)
	cmd.Stdout, cmd.Stderr = output, diagnostic
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("thumbnail preview ffmpeg: %w: %s", err, diagnostic.buffer.String())
	}
	tb := pickerTimeBase.FindStringSubmatch(diagnostic.buffer.String())
	pts := pickerPTS.FindAllStringSubmatch(diagnostic.buffer.String(), -1)
	if len(tb) != 3 {
		return nil, fmt.Errorf("thumbnail preview has no frame time base")
	}
	numerator, _ := strconv.ParseFloat(tb[1], 64)
	denominator, _ := strconv.ParseFloat(tb[2], 64)
	if numerator <= 0 || denominator <= 0 {
		return nil, fmt.Errorf("thumbnail preview has an invalid frame time base")
	}
	var frames []PickerFrame
	for output.buffer.Len() > 0 && len(frames) < count {
		im, err := png.Decode(&output.buffer)
		if err != nil {
			return nil, fmt.Errorf("decode thumbnail preview: %w", err)
		}
		if len(pts) <= len(frames) {
			return nil, fmt.Errorf("thumbnail preview has no frame timestamp")
		}
		p, err := strconv.ParseInt(pts[len(frames)][1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid thumbnail timestamp: %w", err)
		}
		// Seeking uses the first frame at/after the requested instant. Stay
		// just before the decoded PTS so microsecond rounding cannot select the
		// following frame when the cover generator repeats this seek.
		target := math.Max(0, math.Round((at+float64(p)*numerator/denominator)*1e6)/1e6-0.000002)
		if target >= video.VideoStreamDuration || (len(frames) > 0 && target <= frames[len(frames)-1].At) {
			return nil, fmt.Errorf("thumbnail frame timestamps are outside the video or not increasing")
		}
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, im, &jpeg.Options{Quality: 82}); err != nil {
			return nil, err
		}
		frames = append(frames, PickerFrame{At: target, JPEG: encoded.Bytes()})
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("no preview frame at this position")
	}
	return frames, ctx.Err()
}
