package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
	if !log.havePTS {
		return nil, errNoKeyframeDecoded
	}
	if len(jpeg) < 4 || !bytes.HasPrefix(jpeg, []byte{0xff, 0xd8}) || !bytes.HasSuffix(jpeg, []byte{0xff, 0xd9}) {
		return nil, errors.New("ffmpeg returned an invalid JPEG")
	}
	return &keyframeStill{jpeg: jpeg, time: log.pts - log.start, elapsed: elapsed}, nil
}

var showinfoPTSRE = regexp.MustCompile(`\] n:\s*0 .*\bpts_time:(\S+)`)

// keyframeStillLog reads the container start time from ffmpeg's input
// summary and the first frame's timestamp from showinfo, without retaining
// the rest of the log. Metadata lines are indented further than the summary
// line, so tag values cannot impersonate it.
type keyframeStillLog struct {
	partial   []byte
	start     float64
	haveStart bool
	pts       float64
	havePTS   bool
	recent    []string
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
	if !l.haveStart && strings.HasPrefix(line, "  Duration: ") {
		if _, after, ok := strings.Cut(line, ", start: "); ok {
			value, _, _ := strings.Cut(after, ",")
			if start, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				l.start, l.haveStart = start, true
			}
		}
		return
	}
	if strings.Contains(line, "Parsed_showinfo_") {
		if !l.havePTS {
			if match := showinfoPTSRE.FindStringSubmatch(line); match != nil {
				if pts, err := strconv.ParseFloat(match[1], 64); err == nil {
					l.pts, l.havePTS = pts, true
				}
			}
		}
		return
	}
	if line = strings.TrimSpace(line); line != "" {
		l.recent = append(l.recent, line)
		if len(l.recent) > 4 {
			l.recent = l.recent[1:]
		}
	}
}

func (l *keyframeStillLog) errorText() string {
	text := strings.Join(l.recent, "; ")
	if len(text) > 1024 {
		text = text[len(text)-1024:]
	}
	return text
}
