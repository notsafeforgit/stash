package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	stashExec "github.com/stashapp/stash/pkg/exec"
	"github.com/stashapp/stash/pkg/models"
)

// keyframeStillQuality is the MJPEG quantizer scale (2 best, 31 worst). A
// 240p scrub still is typically 5–15 KB at this setting.
const keyframeStillQuality = 5

// keyframeStillMaxBytes bounds the encoded still read from ffmpeg.
const keyframeStillMaxBytes = 16 << 20

var errNoKeyframeDecoded = errors.New("ffmpeg decoded no keyframe")

// keyframeStillFilter scales to the scrub stream's LOW dimensions and converts
// HDR to SDR. Rotation needs no filter: software decoding applies the display
// matrix before these filters, and stored dimensions are display dimensions.
// HDR frames are scaled at their original bit depth before tone mapping, which
// keeps a 4K still inexpensive.
func keyframeStillFilter(vf *models.VideoFile, color videoColorMetadata, maxResolution int) VideoFilter {
	var scale VideoFilter
	if vf.Width > 0 && vf.Height > 0 {
		scale = scale.ScaleMax(vf.Width, vf.Height, maxResolution)
	} else {
		scale = scale.ScaleDimensions(-2, maxResolution)
	}
	if !color.isHDR() {
		return scale
	}
	return softwareToneMapFilter(prependVideoFilter(hdrInputFilter(color), scale))
}

// keyframeStillArgs decodes only keyframes from a software decoder. With
// accurate seeking ffmpeg discards decoded frames before seek, so the result is
// the first keyframe at or after it; without, it is the keyframe the demuxer
// seeks to. -copyts keeps the decoded frame's container timestamp so the
// caller can report the frame's exact scene time.
func keyframeStillArgs(path string, seek float64, accurate bool, filter VideoFilter) Args {
	args := Args{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "info"}
	// Frame threading would queue several packets before the first output.
	args = append(args, "-thread_type", "slice", "-skip_frame", "nokey", "-copyts")
	if !accurate {
		args = args.NoAccurateSeek()
	}
	args = append(args, "-ss", strconv.FormatFloat(max(seek, 0), 'f', 6, 64))
	args = args.Input(path)
	args = append(args, "-map", "0:v:0")
	args = args.VideoFilter(filter.Append("showinfo"))
	args = append(args, "-fps_mode", "passthrough")
	args = args.VideoFrames(1)
	args = args.VideoCodec(VideoCodecMJpeg)
	args = args.FixedQualityScaleVideo(keyframeStillQuality)
	args = args.Format(FormatImage2Pipe)
	return args.Output("pipe:1")
}

type keyframeStill struct {
	jpeg []byte
	// Scene time of the decoded frame, in seconds.
	time    float64
	elapsed time.Duration
}

// extractKeyframeStill runs one bounded ffmpeg. Cancelling ctx kills it.
func (f *FFMpeg) extractKeyframeStill(ctx context.Context, path string, seek float64, accurate bool, filter VideoFilter, timeout time.Duration) (*keyframeStill, error) {
	args := keyframeStillArgs(path, seek, accurate, filter)
	log := &keyframeStillLog{}
	var jpeg []byte
	started := time.Now()
	err := stashExec.RunBounded(ctx, stashExec.OutputLimits{Timeout: timeout, Stdout: keyframeStillMaxBytes, Stderr: 16 << 20}, func(ctx context.Context) *exec.Cmd {
		return stashExec.CommandContext(ctx, f.ffmpeg, args...)
	}, nil, func(stdout io.Reader) error {
		var err error
		jpeg, err = io.ReadAll(stdout)
		return err
	}, log)
	elapsed := time.Since(started)
	if err != nil {
		if detail := log.errorText(); detail != "" {
			return nil, fmt.Errorf("%w: %s", err, detail)
		}
		return nil, err
	}
	if len(log.frames) == 0 {
		return nil, errNoKeyframeDecoded
	}
	if len(jpeg) < 4 || !bytes.HasPrefix(jpeg, []byte{0xff, 0xd8}) || !bytes.HasSuffix(jpeg, []byte{0xff, 0xd9}) {
		return nil, errors.New("ffmpeg returned an invalid JPEG")
	}
	return &keyframeStill{jpeg: jpeg, time: float64(log.frameTimeUS(0))/1e6 - log.start, elapsed: elapsed}, nil
}

// A batch reads its keyframes through the concat demuxer: one short segment
// per keyframe, opened one after another and decoded by a single decoder, so
// a batch needs about the memory of one still. (One input per keyframe would
// keep every decoder alive until exit: about 3 GB for 16 4K HEVC frames.)
//
// Each segment starts keyframeBatchLeadUS before its keyframe's file
// timestamp. Like ffmpeg's -ss heuristic for streams with B-frames (3/23 s),
// the lead keeps MPEG-TS, which seeks by decode timestamp, from resuming
// after the keyframe. Indexed containers resume at the preceding keyframe; a
// select filter drops it before scaling. The segment ends keyframeBatchTailUS
// after the keyframe, before the next one.
const (
	keyframeBatchLeadUS = 131_000
	keyframeBatchTailUS = 2_000
	keyframeBatchSpanUS = keyframeBatchLeadUS + keyframeBatchTailUS
	// Every segment lasts keyframeBatchSpanUS, and the demuxer maps segment
	// i's inpoint to i·span, so wanted frames are output at lead (mod span)
	// within the rounding of their millisecond index time and time base.
	keyframeBatchWindowUS = 1_500
	// Most frames a batch log may describe.
	keyframeBatchMaxFrames = 1024
)

// keyframeConcatScript lists one segment per keyframe file timestamp. The
// script is read from stdin, so paths are file: URLs: they are not resolved
// against pipe:0 and cannot name another protocol.
func keyframeConcatScript(path string, fileTimesUS []int64) ([]byte, error) {
	if strings.ContainsAny(path, "\x00\r\n") {
		return nil, errors.New("path cannot be listed in a concat script")
	}
	quoted := "'" + strings.ReplaceAll("file:"+path, "'", `'\''`) + "'"
	var script bytes.Buffer
	script.WriteString("ffconcat version 1.0\n")
	for _, us := range fileTimesUS {
		fmt.Fprintf(&script, "file %s\ninpoint %s\noutpoint %s\n", quoted, formatMicroseconds(us-keyframeBatchLeadUS), formatMicroseconds(us+keyframeBatchTailUS))
	}
	return script.Bytes(), nil
}

func formatMicroseconds(us int64) string {
	sign := ""
	if us < 0 {
		sign, us = "-", -us
	}
	return fmt.Sprintf("%s%d.%06d", sign, us/1_000_000, us%1_000_000)
}

// keyframeStillsArgs decodes the concat script on stdin keyframe-only. The
// metadata filter logs each frame's segment start so frames can be matched to
// segments whatever order the decoder returns them in.
func keyframeStillsArgs(filter VideoFilter) Args {
	args := Args{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "info", "-copyts"}
	args = append(args, "-thread_type", "slice", "-skip_frame", "nokey",
		"-f", "concat", "-safe", "0", "-auto_convert", "0", "-segment_time_metadata", "1",
		"-protocol_whitelist", "file,pipe")
	args = args.Input("pipe:0")
	args = append(args, "-map", "0:v:0")
	half := formatMicroseconds(keyframeBatchSpanUS / 2)
	chain := VideoFilter(fmt.Sprintf("select='lt(abs(mod(t-%s+%s,%s)-%s),%s)'",
		formatMicroseconds(keyframeBatchLeadUS), half, formatMicroseconds(keyframeBatchSpanUS), half, formatMicroseconds(keyframeBatchWindowUS)))
	if filter != "" {
		chain = chain.Append(string(filter))
	}
	chain = chain.Append("metadata=mode=print:key=lavf.concatdec.start_time").Append("showinfo")
	args = args.VideoFilter(chain)
	args = append(args, "-fps_mode", "passthrough")
	args = args.VideoCodec(VideoCodecMJpeg)
	args = args.FixedQualityScaleVideo(keyframeStillQuality)
	args = args.Format(FormatImage2Pipe)
	return args.Output("pipe:1")
}

// extractKeyframeStills returns a JPEG for each keyframe (scene time in ms),
// nil where its segment yielded no frame at that time. startUS is the
// container start. An error means the run failed.
func (f *FFMpeg) extractKeyframeStills(ctx context.Context, path string, startUS int64, targets []int64, filter VideoFilter, timeout time.Duration) ([][]byte, time.Duration, error) {
	fileTimes := make([]int64, len(targets))
	for i, ms := range targets {
		fileTimes[i] = ms*1000 + startUS
	}
	script, err := keyframeConcatScript(path, fileTimes)
	if err != nil {
		return nil, 0, err
	}
	args := keyframeStillsArgs(filter)
	log := &keyframeStillLog{segments: map[int]int64{}}
	var stream []byte
	started := time.Now()
	err = stashExec.RunBounded(ctx, stashExec.OutputLimits{Timeout: timeout, Stdout: keyframeStillMaxBytes, Stderr: 16 << 20}, func(ctx context.Context) *exec.Cmd {
		return stashExec.CommandContext(ctx, f.ffmpeg, args...)
	}, bytes.NewReader(script), func(stdout io.Reader) error {
		var err error
		stream, err = io.ReadAll(stdout)
		return err
	}, log)
	elapsed := time.Since(started)
	if err != nil {
		if detail := log.errorText(); detail != "" {
			return nil, elapsed, fmt.Errorf("%w: %s", err, detail)
		}
		return nil, elapsed, err
	}
	images, err := splitJPEGStream(stream)
	if err != nil {
		return nil, elapsed, err
	}
	if log.invalid || len(images) != len(log.frames) {
		return nil, elapsed, fmt.Errorf("ffmpeg returned %d images for %d logged frames", len(images), len(log.frames))
	}
	stills := make([][]byte, len(targets))
	best := make([]int64, len(targets))
	for n := range log.frames {
		segment, ok := log.segments[n]
		if !ok || segment < 0 || segment%keyframeBatchSpanUS != 0 || segment/keyframeBatchSpanUS >= int64(len(targets)) {
			continue
		}
		slot := segment / keyframeBatchSpanUS
		// The demuxer maps each segment's inpoint to its segment start.
		sceneUS := log.frameTimeUS(n) - segment + fileTimes[slot] - keyframeBatchLeadUS - startUS
		diff := sceneUS - targets[slot]*1000
		if diff < 0 {
			diff = -diff
		}
		if diff <= keyframeCanonicalToleranceUS && (stills[slot] == nil || diff < best[slot]) {
			stills[slot], best[slot] = images[n], diff
		}
	}
	return stills, elapsed, nil
}

// keyframeCanonicalToleranceUS is how far a decoded frame may lie from its
// millisecond index time and still be that keyframe.
const keyframeCanonicalToleranceUS = 2_000

// splitJPEGStream splits concatenated JPEGs. It follows marker segment lengths
// up to each scan, then scans the entropy-coded data, in which 0xFF is always
// a stuffed byte or a restart marker, until the next marker.
func splitJPEGStream(data []byte) ([][]byte, error) {
	var images [][]byte
	for len(data) > 0 {
		end, err := jpegLength(data)
		if err != nil {
			return nil, fmt.Errorf("image %d: %w", len(images), err)
		}
		images = append(images, data[:end:end])
		data = data[end:]
	}
	return images, nil
}

func jpegLength(data []byte) (int, error) {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 0, errors.New("missing JPEG start marker")
	}
	i := 2
	for {
		if i+2 > len(data) || data[i] != 0xff {
			return 0, errors.New("truncated JPEG header")
		}
		marker := data[i+1]
		switch {
		case marker == 0xff:
			// Fill byte before a marker.
			i++
			continue
		case marker == 0xd9:
			return i + 2, nil
		case marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7):
			i += 2
			continue
		}
		if i+4 > len(data) {
			return 0, errors.New("truncated JPEG segment")
		}
		length := int(data[i+2])<<8 | int(data[i+3])
		if length < 2 || i+2+length > len(data) {
			return 0, errors.New("invalid JPEG segment length")
		}
		i += 2 + length
		if marker != 0xda {
			continue
		}
		for {
			if i+1 >= len(data) {
				return 0, errors.New("truncated JPEG scan")
			}
			if data[i] != 0xff {
				i++
				continue
			}
			next := data[i+1]
			if next == 0x00 || (next >= 0xd0 && next <= 0xd7) {
				i += 2
				continue
			}
			if next == 0xff {
				i++
				continue
			}
			break
		}
	}
}

var (
	showinfoTimeBaseRE = regexp.MustCompile(`\] config in time_base: (\d+)/(\d+)`)
	showinfoFrameRE    = regexp.MustCompile(`\] n:\s*(\d+) pts:\s*(-?\d+) pts_time:(\S+)`)
	metadataFrameRE    = regexp.MustCompile(`\] frame:(\d+)\s`)
	metadataSegmentRE  = regexp.MustCompile(`\] lavf\.concatdec\.start_time=(-?\d+)\s*$`)
)

type keyframeLogFrame struct {
	pts     int64
	ptsTime float64
}

// keyframeStillLog reads ffmpeg's log without retaining it: the container
// start time from the input summary, showinfo's time base and frames in
// output order and, for batches, each frame's concat segment start from the
// metadata filter. Times use the integer pts, because ffmpeg before 7.0 prints
// pts_time with only six significant digits. Metadata lines are indented
// further than the summary line, so tag values cannot impersonate it.
type keyframeStillLog struct {
	partial   []byte
	start     float64
	haveStart bool
	tbNum     int64
	tbDen     int64
	frames    []keyframeLogFrame
	// Segment start (µs) by frame number; nil for single extractions.
	segments  map[int]int64
	metaFrame int
	// Frames were logged out of sequence or beyond keyframeBatchMaxFrames.
	invalid bool
	recent  []string
}

func (l *keyframeStillLog) Write(p []byte) (int, error) {
	l.partial = append(l.partial, p...)
	data := l.partial
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		l.line(strings.TrimRight(string(data[:i]), "\r"))
		data = data[i+1:]
	}
	if len(data) > 64<<10 {
		data = data[len(data)-64<<10:]
	}
	l.partial = slices.Clone(data)
	return len(p), nil
}

func (l *keyframeStillLog) line(line string) {
	switch {
	case !l.haveStart && strings.HasPrefix(line, "  Duration: "):
		if _, after, ok := strings.Cut(line, ", start: "); ok {
			value, _, _ := strings.Cut(after, ",")
			if start, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				l.start, l.haveStart = start, true
			}
		}
	case strings.Contains(line, "Parsed_showinfo_"):
		l.showinfo(line)
	case l.segments != nil && strings.Contains(line, "Parsed_metadata_"):
		// The metadata filter logs a frame line followed by its entries.
		if match := metadataFrameRE.FindStringSubmatch(line); match != nil {
			l.metaFrame, _ = strconv.Atoi(match[1])
		} else if match := metadataSegmentRE.FindStringSubmatch(line); match != nil {
			if start, err := strconv.ParseInt(match[1], 10, 64); err == nil && l.metaFrame < keyframeBatchMaxFrames {
				l.segments[l.metaFrame] = start
			}
		}
	default:
		if line = strings.TrimSpace(line); line != "" {
			l.recent = append(l.recent, line)
			if len(l.recent) > 4 {
				l.recent = l.recent[1:]
			}
		}
	}
}

func (l *keyframeStillLog) showinfo(line string) {
	if match := showinfoTimeBaseRE.FindStringSubmatch(line); match != nil {
		num, numErr := strconv.ParseInt(match[1], 10, 64)
		den, denErr := strconv.ParseInt(match[2], 10, 64)
		if numErr == nil && denErr == nil && num > 0 && den > 0 && l.tbDen == 0 {
			l.tbNum, l.tbDen = num, den
		}
		return
	}
	match := showinfoFrameRE.FindStringSubmatch(line)
	if match == nil {
		return
	}
	n, nErr := strconv.Atoi(match[1])
	pts, ptsErr := strconv.ParseInt(match[2], 10, 64)
	ptsTime, timeErr := strconv.ParseFloat(match[3], 64)
	if nErr != nil || ptsErr != nil || timeErr != nil || n != len(l.frames) || n >= keyframeBatchMaxFrames {
		l.invalid = true
		return
	}
	l.frames = append(l.frames, keyframeLogFrame{pts: pts, ptsTime: ptsTime})
}

// frameTimeUS is frame n's timestamp in microseconds.
func (l *keyframeStillLog) frameTimeUS(n int) int64 {
	frame := l.frames[n]
	if l.tbDen > 0 {
		return int64(math.Round(float64(frame.pts) * 1e6 * float64(l.tbNum) / float64(l.tbDen)))
	}
	return int64(math.Round(frame.ptsTime * 1e6))
}

func (l *keyframeStillLog) errorText() string {
	text := strings.Join(l.recent, "; ")
	if len(text) > 1024 {
		text = text[len(text)-1024:]
	}
	return text
}
