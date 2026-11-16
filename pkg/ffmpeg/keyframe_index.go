package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	stashExec "github.com/stashapp/stash/pkg/exec"
)

// keyframeIndexRevision identifies how scans select and round keyframes. A
// change invalidates persisted indexes and every derived client version.
const keyframeIndexRevision = 1

// keyframeIndexEncoding is the packed format's leading byte.
const keyframeIndexEncoding = 1

// Larger indexes are thinned to a regular minimum spacing. Ordinary long-GOP
// sources never reach this; all-intra sources would otherwise list every frame.
const maxIndexedKeyframes = 50_000

// The raw scan holds at most this many keyframe timestamps before thinning.
const maxScannedKeyframes = 4_000_000

var errNoKeyframes = errors.New("no decodable keyframes in the first video stream")

// keyframeIndex lists the first video stream's sync samples in scene time:
// the timeline `ffmpeg -ss` input seeking uses, i.e. packet presentation
// time minus the container's start time, in whole milliseconds.
type keyframeIndex struct {
	times []int64
	// Stream color tags observed by the scan. Older library rows can lack
	// them, so they decide whether stills need SDR tone mapping.
	color videoColorMetadata
}

// atOrBefore returns the greatest keyframe time ≤ ms, or the first keyframe
// when ms precedes it.
func (k *keyframeIndex) atOrBefore(ms int64) int64 {
	i, _ := slices.BinarySearch(k.times, ms+1)
	if i == 0 {
		return k.times[0]
	}
	return k.times[i-1]
}

// encodeKeyframeIndex packs the index as delta-encoded unsigned varints,
// about two bytes per keyframe.
func encodeKeyframeIndex(k *keyframeIndex) []byte {
	buf := make([]byte, 0, 32+2*len(k.times))
	buf = append(buf, keyframeIndexEncoding)
	for _, tag := range []string{k.color.Range, k.color.Space, k.color.Transfer, k.color.Primaries} {
		buf = binary.AppendUvarint(buf, uint64(len(tag)))
		buf = append(buf, tag...)
	}
	buf = binary.AppendUvarint(buf, uint64(len(k.times)))
	previous := int64(0)
	for _, ms := range k.times {
		buf = binary.AppendUvarint(buf, uint64(ms-previous))
		previous = ms
	}
	return buf
}

func decodeKeyframeIndex(data []byte) (*keyframeIndex, error) {
	r := bytes.NewReader(data)
	if version, err := r.ReadByte(); err != nil || version != keyframeIndexEncoding {
		return nil, errors.New("unsupported keyframe index encoding")
	}
	var tags [4]string
	for i := range tags {
		n, err := binary.ReadUvarint(r)
		if err != nil || n > 64 || n > uint64(r.Len()) {
			return nil, errors.New("invalid keyframe index color tag")
		}
		tag := make([]byte, n)
		if _, err := io.ReadFull(r, tag); err != nil {
			return nil, err
		}
		tags[i] = string(tag)
	}
	count, err := binary.ReadUvarint(r)
	if err != nil || count == 0 || count > maxIndexedKeyframes || count > uint64(r.Len()) {
		return nil, errors.New("invalid keyframe index length")
	}
	times := make([]int64, count)
	previous := int64(0)
	for i := range times {
		delta, err := binary.ReadUvarint(r)
		if err != nil || delta > math.MaxInt32 || (i > 0 && delta == 0) {
			return nil, errors.New("invalid keyframe index entry")
		}
		previous += int64(delta)
		times[i] = previous
	}
	if r.Len() != 0 {
		return nil, errors.New("trailing keyframe index data")
	}
	return &keyframeIndex{times: times, color: videoColorMetadata{Range: tags[0], Space: tags[1], Transfer: tags[2], Primaries: tags[3]}}, nil
}

// scanKeyframes lists sync samples with a demux-only packet scan. Nothing is
// decoded, but most containers still read every packet, so the caller bounds
// the run time.
func (f *FFProbe) scanKeyframes(ctx context.Context, path string, timeout time.Duration) (*keyframeIndex, error) {
	args := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "packet=pts_time,dts_time,flags:stream=color_range,color_space,color_transfer,color_primaries:format=start_time",
		"-of", "compact=p=1:nk=0",
		"-i", path,
	}
	var (
		raw   []float64
		start float64
		color videoColorMetadata
	)
	consume := func(stdout io.Reader) error {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 4096), 64<<10)
		for scanner.Scan() {
			section, fields, ok := strings.Cut(scanner.Text(), "|")
			if !ok {
				continue
			}
			values := compactFields(fields)
			switch section {
			case "packet":
				flags := values["flags"]
				// Discarded (edit-list preroll) and corrupt keyframes are never
				// displayed, so a keyframe-only decode cannot show them.
				if !strings.Contains(flags, "K") || strings.ContainsAny(flags, "DC") {
					continue
				}
				pts, err := strconv.ParseFloat(values["pts_time"], 64)
				if err != nil {
					pts, err = strconv.ParseFloat(values["dts_time"], 64)
				}
				if err != nil || math.IsNaN(pts) || math.IsInf(pts, 0) {
					continue
				}
				if len(raw) >= maxScannedKeyframes {
					return fmt.Errorf("more than %d keyframes", maxScannedKeyframes)
				}
				raw = append(raw, pts)
			case "stream":
				color = videoColorMetadata{
					Range:     normalizeColorTag(values["color_range"]),
					Space:     normalizeColorTag(values["color_space"]),
					Transfer:  normalizeColorTag(values["color_transfer"]),
					Primaries: normalizeColorTag(values["color_primaries"]),
				}
			case "format":
				// ffmpeg seeks relative to the container start; N/A means zero.
				if value, err := strconv.ParseFloat(values["start_time"], 64); err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
					start = value
				}
			}
		}
		return scanner.Err()
	}
	var stderr bytes.Buffer
	err := stashExec.RunBounded(ctx, stashExec.OutputLimits{Timeout: timeout, Stdout: 1 << 30, Stderr: 64 << 10}, func(ctx context.Context) *exec.Cmd {
		return stashExec.CommandContext(ctx, f.path, args...)
	}, nil, consume, &stderr)
	if err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			if len(message) > 512 {
				message = message[len(message)-512:]
			}
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	return newKeyframeIndex(raw, start, color)
}

// compactFields parses ffprobe's `compact` writer fields. The values read here
// (numbers, flags and color names) never contain its escaped separators.
func compactFields(fields string) map[string]string {
	values := make(map[string]string, 4)
	for _, field := range strings.Split(fields, "|") {
		if key, value, ok := strings.Cut(field, "="); ok {
			values[key] = value
		}
	}
	return values
}

func newKeyframeIndex(raw []float64, start float64, color videoColorMetadata) (*keyframeIndex, error) {
	times := make([]int64, 0, len(raw))
	for _, pts := range raw {
		ms := int64(math.Round((pts - start) * 1000))
		if ms < 0 || ms > math.MaxInt32 {
			continue
		}
		times = append(times, ms)
	}
	slices.Sort(times)
	times = slices.Compact(times)
	if len(times) == 0 {
		return nil, errNoKeyframes
	}
	if len(times) > maxIndexedKeyframes {
		spacing := (times[len(times)-1]-times[0])/(maxIndexedKeyframes-1) + 1
		thinned := times[:1]
		for _, ms := range times[1:] {
			if ms-thinned[len(thinned)-1] >= spacing {
				thinned = append(thinned, ms)
			}
		}
		times = thinned
	}
	return &keyframeIndex{times: slices.Clip(times), color: color}, nil
}
