package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/utils"
)

// transcodeDebug forces every `[transcode]`-prefixed log line that
// routes through TranscodeTracef / TranscodeDebugf to emit at Info
// level so it shows up regardless of the global log level. Toggle via
// the `STASH_TRANSCODE_DEBUG` env var. Useful for diagnosing HLS
// pipeline behaviour without having to flip the entire server to Trace
// and drown in SQL output.
//
// TEMPORARY DIAGNOSTIC — remove this var, the helpers below, and
// restore the call sites to `logger.Tracef` / `logger.Debugf` once the
// transcode pipeline is stable. Search for `TranscodeDebugf` /
// `TranscodeTracef` / `transcodeDebug` to find every callsite that
// needs reverting.
var transcodeDebug = os.Getenv("STASH_TRANSCODE_DEBUG") != ""

func TranscodeTracef(format string, args ...interface{}) {
	if transcodeDebug {
		logger.Infof(format, args...)
		return
	}
	logger.Tracef(format, args...)
}

func TranscodeDebugf(format string, args ...interface{}) {
	if transcodeDebug {
		logger.Infof(format, args...)
		return
	}
	logger.Debugf(format, args...)
}

const (
	// Maximum idle time between any v3 client touchpoint (segment fetch
	// or `streams.keepalive` ping) and a reap of the stream + its
	// cached segments. The client pings keepalive every ~15 s while
	// paused, so 60 s tolerates several dropped pings.
	v3MaxIdleTime = 60 * time.Second

	// Lookahead-buffer gate for SIGSTOP/SIGCONT pause-resume of the
	// v3 ffmpeg process. Values are expressed in segment units.
	v3SuspendLookaheadSegments = 60 / segmentLength
	v3ResumeLookaheadSegments  = 15 / segmentLength
)

type V3StreamType struct {
	Name        string
	SegmentType *V3SegmentType
	Args        func(codec VideoCodec, segment int, videoFilter VideoFilter, videoOnly bool, outputDir string, frameRate float64) Args
}

// hlsGopSize returns the GOP size (in frames) the encoder should use so
// keyframes land at every segmentLength seconds of source content. Passed
// to ffmpeg as `-g <N>` (and `-keyint_min <N>` to forbid shrinking on
// scene changes). Without this, hardware encoders (notably h264_qsv) emit
// keyframes only at their default GOP boundary (~10 s for QSV), which
// makes the HLS muxer split segments at multi-segment intervals — the
// `-force_key_frames` directive that libx264 honours is silently ignored
// by QSV. With `-hls_flags split_by_time` removed (fMP4 needs strict
// per-fragment monotonicity), the segment splitter only cuts at
// keyframes, so a 10 s GOP yields 10 s segments — far outside what SPF's
// buffer math (`bufferDuration: 30`, `keepSegments: 2`) is sized for.
func hlsGopSize(frameRate float64) int {
	if frameRate <= 0 || math.IsInf(frameRate, 0) || math.IsNaN(frameRate) {
		// Fallback: assume 30 fps when probe data is missing. With the
		// default 2 s segment length this yields a 60-frame GOP, which
		// approximates 2 s on most real-world content (24-60 fps) within
		// ~20%.
		frameRate = 30
	}
	gop := int(math.Round(frameRate * float64(segmentLength)))
	if gop < 1 {
		gop = 1
	}
	return gop
}

// hlsSegmentDuration returns the actual wall-clock duration of one HLS
// segment, in seconds, given the source frame rate. Equal to
// `hlsGopSize(frameRate) / frameRate` — i.e. the GOP size in frames
// divided by the frame rate. This is the value the encoder produces:
// each segment ends on the IDR boundary the GOP requested, so its real
// duration is `gopFrames / frameRate`, not the nominal `segmentLength`.
//
// For integer frame rates (24 / 30 / 60 fps) this collapses to exactly
// `segmentLength`. For NTSC fractionals (23.976 / 29.97 / 59.94 fps) it
// drifts: 59.94 fps → 120/59.94 = 2.0020 s segments. Threading this real
// value through the `-output_ts_offset`, `-hls_time`, EXTINF, and
// `?start=` floor keeps every PTS / playlist time consistent with the
// bytes ffmpeg actually emits — without it, segment N's declared time
// drifts from its actual PTS by `N · 0.002 s`, which Safari + iOS
// AVFoundation react to as visible frame-pacing judder on long-running
// playback.
//
// The frontend mirrors this calculation in `hls.ts` so its segment-
// boundary floor and `effective_trim` math agree with the server.
func hlsSegmentDuration(frameRate float64) float64 {
	if frameRate <= 0 || math.IsInf(frameRate, 0) || math.IsNaN(frameRate) {
		return float64(segmentLength)
	}
	gop := hlsGopSize(frameRate)
	if gop <= 0 {
		return float64(segmentLength)
	}
	return float64(gop) / frameRate
}

// Track identifies a demuxed HLS rendition. With ffmpeg's -var_stream_map
// each variant produces its own init segment + .m4s media segments under a
// per-track filename prefix; the master playlist references them as a video
// variant + an EXT-X-MEDIA TYPE=AUDIO rendition.
//
// Demuxing the source into separate audio + video HLS streams (rather than
// muxing both into one .m4s) is what lets MSE-based clients (@videojs/spf)
// create one SourceBuffer per track. Chrome's MSE rejects a muxed init
// segment whose moov box advertises a track that wasn't declared on the
// SourceBuffer's codec string, which is what happened when we emitted
// avc1+mp4a in a single stream.
type Track string

const (
	TrackVideo Track = "video"
	TrackAudio Track = "audio"
)

func ParseTrack(s string) (Track, bool) {
	switch Track(s) {
	case TrackVideo, TrackAudio:
		return Track(s), true
	}
	return "", false
}

var (
	// V3StreamTypeHLS — re-encode the source to H.264 + AAC and serve as
	// fragmented-MP4 HLS. The transcode fallback when the source's codecs
	// aren't decodable in the target browser. fMP4 (not MPEG-TS) so the
	// segments are addressable by both Safari's native HLS and MSE-based
	// HLS clients (e.g. @videojs/spf) — MPEG-TS is fine for native HLS but
	// MSE clients only consume fMP4.
	V3StreamTypeHLS = &V3StreamType{
		Name:        "hls",
		SegmentType: V3SegmentTypeFMP4,
		Args: func(codec VideoCodec, segment int, videoFilter VideoFilter, videoOnly bool, outputDir string, frameRate float64) (args Args) {
			args = CodecInit(codec)
			gop := hlsGopSize(frameRate)
			segDur := hlsSegmentDuration(frameRate)
			// Pin the output frame rate. Without an explicit `-r` here,
			// libx264 falls back to the muxer/codec default (25 fps) when
			// the input demuxer can't surface a frame rate the encoder
			// trusts — observed on iPhone-recorded HLG HDR AV1 (avg
			// frame rate `19001/317`, time base `1/1000`). That defaults
			// libx264's timebase to `1/12800` with 512 ticks per frame
			// (40 ms), and `-fps_mode auto` then drops ~70% of source
			// frames to fit the 25 fps slot rate — the dropped-frame
			// pattern is the visible judder users see on transcode
			// while direct stream plays smoothly.
			//
			// Pair with `-fps_mode cfr` so frames pass through to a
			// uniform output cadence at the declared rate; for sources
			// whose actual PTS deltas already match (every CFR source),
			// this is a 1:1 pass-through with no drops or duplicates.
			// `frameRate` is the float we have at this layer (rounded
			// to 2 decimals); for NTSC fractionals (59.94) the tiny
			// rounding vs the true 60000/1001 yields ~1 frame drop per
			// hour — well below visual threshold.
			rateArg := "25"
			if frameRate > 0 {
				rateArg = fmt.Sprintf("%g", frameRate)
			}
			args = append(args,
				"-r", rateArg,
				"-fps_mode:v", "cfr",
				"-flags", "+cgop",
				// `-g` + `-keyint_min` is the universal keyframe-interval
				// control that all H.264 encoders honour, including the
				// hardware ones that ignore `-force_key_frames` (notably
				// h264_qsv). Setting `keyint_min == g` forbids the encoder
				// from shrinking the interval on scene changes, so segments
				// stay a consistent length.
				"-g", fmt.Sprint(gop),
				"-keyint_min", fmt.Sprint(gop),
				// libx264 honours this expression; HW encoders ignore it
				// silently. Kept because it tightens libx264's IDR placement
				// relative to the GOP boundary (`-g` is a maximum), and the
				// HW encoders are unaffected. `segDur` (not `segmentLength`)
				// because for non-integer NTSC frame rates the actual segment
				// duration drifts: 59.94 fps → 120-frame GOP → 2.0020 s
				// segments. Passing 2.0 here would push libx264's IDR target
				// 0.002 s before the GOP boundary on each segment.
				"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%g)", segDur),
			)
			// Normalize per-track PTS to zero at the encoder so the two
			// tracks align inside each segment. Source files (especially
			// iOS-recorded M4V with a container-level audio edit list) can
			// deliver the audio track ~1.2 s later than the video track
			// after `-ss` input seek. Native HLS players play the video's
			// declared PTS as-is; with skewed start times on the two
			// tracks, Safari's AVFoundation pipeline stalls the video track
			// while the audio keeps going. setpts/asetpts forces both to
			// start at 0 independently.
			videoFilter = videoFilter.Append("setpts=PTS-STARTPTS")
			args = args.VideoFilter(videoFilter)
			if videoOnly {
				args = append(args, "-an")
			} else {
				args = append(args,
					"-af", "asetpts=PTS-STARTPTS",
					"-c:a", "aac",
					"-ac", "2",
				)
			}
			// Transcode: both tracks re-encoded with setpts/asetpts → both need
			// the segment-aligned offset to undo the per-run PTS reset.
			args = append(args, hlsSegmentArgs(segment, videoOnly, outputDir, frameRate, true, true)...)
			return
		},
	}
	// V3StreamTypeHLSCopyFMP4 remuxes (no re-encode) the source's video and audio
	// tracks into fragmented-MP4 HLS segments. The fast path when the source's
	// codecs are already browser-decodable: AV1/HEVC/H.264 + Opus/AAC. Sources
	// that fall outside this set fall back to the full re-encode at
	// V3StreamTypeHLS above.
	V3StreamTypeHLSCopyFMP4 = &V3StreamType{
		Name:        "hls-copy-fmp4",
		SegmentType: V3SegmentTypeFMP4,
		Args: func(codec VideoCodec, segment int, videoFilter VideoFilter, videoOnly bool, outputDir string, frameRate float64) (args Args) {
			// CodecInit(VideoCodecCopy) sets `-c:v copy`; we leave the video
			// bitstream untouched. No video filter (would force decode).
			args = CodecInit(codec)
			if videoOnly {
				args = append(args, "-an")
			} else {
				// `-c:a copy` — pass the audio bitstream through. Caller gates
				// on source audio codec being playable in fMP4 by the browser
				// (AAC / Opus for Safari). If the source audio were something
				// Safari can't decode, we'd either re-encode here or not offer
				// this endpoint at all — the gate lives in GetSceneStreamPaths.
				args = append(args, "-c:a", "copy")
			}
			// Codec-copy on both tracks: no filter graph runs, so `-copyts`
			// hands the source's absolute (segment-aligned) PTS straight
			// through to the muxer. Neither track needs `-output_ts_offset`
			// — adding it would double-count and break far-forward seeks
			// (segments would emit at PTS 2·N·segDur while the playlist
			// declares them at N·segDur).
			args = append(args, hlsSegmentArgs(segment, videoOnly, outputDir, frameRate, false, false)...)
			return
		},
	}
	// V3StreamTypeHLSCopyFMP4AAC copies the source video bitstream untouched and
	// re-encodes the audio track to AAC. Targets browsers that can demux the
	// source video in fMP4 but reject the source audio there (notably iOS
	// Safari's ManagedMediaSource, which won't accept Opus-in-MP4 even on
	// hardware that decodes Opus fine in WebM/Ogg). Falling all the way back
	// to the full H.264 transcode means losing native AV1/HEVC hardware
	// decode and paying real-time encode cost on the server; this variant
	// preserves both.
	V3StreamTypeHLSCopyFMP4AAC = &V3StreamType{
		Name:        "hls-copy-fmp4-aac",
		SegmentType: V3SegmentTypeFMP4,
		Args: func(codec VideoCodec, segment int, videoFilter VideoFilter, videoOnly bool, outputDir string, frameRate float64) (args Args) {
			_ = videoFilter         // unused: -c:v copy can't run a filter graph
			args = CodecInit(codec) // codec=VideoCodecCopy → -c:v copy
			if videoOnly {
				args = append(args, "-an")
			} else {
				// asetpts/-c:a aac mirrors the audio half of V3StreamTypeHLS:
				// the audio re-encode normalises its first-frame PTS to 0
				// inside the run so output_ts_offset can pin the segment-N
				// PTS deterministically across ffmpeg restarts. Stereo
				// downmix matches the transcode path (some sources are 5.1
				// AAC source — the AAC encoder default-mixes those, but
				// being explicit here matches V3StreamTypeHLS).
				args = append(args,
					"-af", "asetpts=PTS-STARTPTS",
					"-c:a", "aac",
					"-ac", "2",
				)
			}
			// Video is codec-copy (source-absolute PTS via -copyts → no
			// offset needed). Audio is re-encoded with asetpts resetting
			// each run's first PTS to 0, so audio needs the segment-aligned
			// offset to land back on its playlist time. Per-track via
			// `-output_ts_offset:a`.
			args = append(args, hlsSegmentArgs(segment, videoOnly, outputDir, frameRate, false, true)...)
			return
		},
	}
)

// hlsSegmentArgs returns the shared HLS muxer args used by both transcode
// and codec-copy variants. With -var_stream_map ffmpeg writes per-track
// playlists/init segments/media segments using the %v placeholder; we
// don't actually serve the auto-generated playlists (we hand-write our
// own master + per-track media playlists with the right ?start= trim
// behaviour), but the per-track init.mp4 + .m4s files on disk are what
// the route handlers serve.
func hlsSegmentArgs(segment int, videoOnly bool, outputDir string, frameRate float64, videoNeedsOffset, audioNeedsOffset bool) Args {
	// var_stream_map: split the input into one variant per track. The
	// audio variant is tagged into rendition group "aac" so ffmpeg's auto-
	// generated master (which we ignore) is well-formed; clients see our
	// hand-written master where the same grouping is applied.
	streamMap := "v:0,agroup:aac,name:" + string(TrackVideo) +
		" a:0,agroup:aac,default:yes,name:" + string(TrackAudio)
	if videoOnly {
		streamMap = "v:0,name:" + string(TrackVideo)
	}
	// ffmpeg's HLS muxer substitutes %v in -hls_segment_filename and the
	// playlist filename even with a single variant, but does NOT substitute
	// %v in -hls_fmp4_init_filename in that case — it writes the file to
	// disk with a literal `%v` and `checkSegments` then can't find the
	// expected `.init_video.mp4` to promote. Use a literal track name when
	// there's only one variant so ffmpeg writes the right file.
	initFilename := ".init_%v.mp4"
	if videoOnly {
		initFilename = ".init_" + string(TrackVideo) + ".mp4"
	}
	// Real per-segment duration. Equal to `segmentLength` for integer
	// frame rates (24/30/60). For NTSC fractionals (23.976/29.97/59.94)
	// the encoder produces segments slightly longer than the nominal
	// `segmentLength` (e.g. 59.94 fps → 120/59.94 = 2.0020 s) because
	// it can only end a segment at an IDR boundary, and the IDR cadence
	// is fixed at `hlsGopSize(frameRate)` frames. Threading the real
	// value through `-output_ts_offset` and `-hls_time` keeps the
	// segment's declared time aligned with the IDR-boundary it actually
	// closes on; without this, segment N's declared time drifts from
	// its actual PTS by `N · 0.002 s` and the player's seek + buffer
	// math diverges from what's on disk.
	segDur := hlsSegmentDuration(frameRate)
	args := Args{
		"-sn",
		// `-copyts` keeps input demuxer timestamps flowing through the
		// pipeline so the source's frame-to-frame PTS deltas survive
		// intact. Without it ffmpeg's muxer can rebuild timestamps from
		// its frame counter, which on a VFR source (AV1 / iOS-recorded
		// video) coerces output to a synthetic cadence and reads as
		// visible frame-pacing stutter. The setpts/asetpts filters above
		// pin the per-track baseline to 0 (iOS edit-list skew fix); the
		// frame-to-frame deltas are what -copyts preserves.
		"-copyts",
		// `make_non_negative` (NOT `make_zero`): only shift when PTS
		// goes negative. `make_zero` nullifies -output_ts_offset on
		// every fresh ffmpeg run, producing each run's segments at PTS
		// 0 regardless of index — fine in isolation but causes a PTS
		// discontinuity across run boundaries that Safari's native HLS
		// can't reconcile without EXT-X-DISCONTINUITY markers.
		"-avoid_negative_ts", "make_non_negative",
	}
	// Align each segment's output PTS to its manifest position so PTS
	// stays consistent across segments produced by different transcode
	// runs (e.g. user seeks past the cached range → new run starts at
	// segment 80, emits PTS 160s, contiguous with cached segments from
	// the earlier run rather than restarting at 0). Multiplied by
	// `segDur` (the real per-segment duration) rather than the nominal
	// `segmentLength` so the offset matches the EXTINF the playlist
	// will declare.
	//
	// Applied per-track. Re-encoded tracks need it because their
	// `setpts=PTS-STARTPTS` / `asetpts=PTS-STARTPTS` filter resets the
	// first PTS of each run to 0, and the offset shifts that back to
	// the segment-aligned playlist time. Codec-copy tracks don't need
	// it: with no filter graph allowed, `-copyts` passes the source's
	// absolute (already segment-aligned) PTS through unchanged. Adding
	// the offset on top of that double-counts — a far-forward seek that
	// restarts ffmpeg at segment N emits segments at PTS `2·N·segDur`
	// while the playlist still declares them at `N·segDur`, and native
	// HLS players (iOS Safari) stall the seek at `readyState=1` because
	// the fMP4 `baseMediaDecodeTime` doesn't match the playlist time.
	tsOffset := fmt.Sprintf("%g", float64(segment)*segDur)
	switch {
	case videoNeedsOffset && (audioNeedsOffset || videoOnly):
		// Both tracks (or video-only) — apply the un-qualified flag for
		// brevity.
		args = append(args, "-output_ts_offset", tsOffset)
	case videoNeedsOffset:
		args = append(args, "-output_ts_offset:v", tsOffset)
	case audioNeedsOffset && !videoOnly:
		args = append(args, "-output_ts_offset:a", tsOffset)
	}
	args = append(args,
		"-f", "hls",
		"-start_number", fmt.Sprint(segment),
		"-hls_time", fmt.Sprintf("%g", segDur),
		// Both media segments and the fMP4 init file are written
		// under dot-prefixed names (`.<track>_<idx>.m4s` and
		// `.init_<track>.mp4`) and only promoted to their final,
		// un-prefixed names by `checkSegments` once the file is
		// fully written. The promotion is idempotent — first run
		// wins, a re-run's duplicate is discarded — which is what
		// keeps the bytes at a given segment index stable across
		// the lifetime of the cache directory. ffmpeg's
		// `+temp_file` flag handles the atomicity of each
		// individual rename (writes to `<name>.tmp` and renames to
		// `<name>` on close), but it overwrites existing files
		// during the rename, so it cannot enforce the "first run
		// wins" invariant on its own — that's why the dot-prefix
		// scheme stages writes off to the side and `checkSegments`
		// arbitrates the promotion.
		//
		// Without this, every ffmpeg restart (e.g. buffer-full →
		// stop, then a later segment request → fresh start) would
		// rewrite already-on-disk segments with different AAC
		// priming and PTS, leaving the player with a buffer
		// patchwork of inconsistent timing and a stall at the
		// first restart boundary.
		//
		// NOT `split_by_time`. With fMP4 + MSE the player demands
		// strict per-fragment frame-PTS monotonicity: each segment's
		// frames must start strictly after the previous segment's
		// last frame. `split_by_time` lets ffmpeg cut a segment at
		// its time boundary even when no keyframe is there, which
		// opens the next segment with frames from the previous GOP
		// and creates a small overlap in PTS at every boundary.
		// Chrome's video renderer reacts by dropping the
		// "out-of-order" frames ("Dropping frame with timestamp X,
		// which is earlier than the last rendered frame Y") and
		// playback stalls a few seconds in. v2.5 ran MPEG-TS, which
		// is tolerant of per-fragment overlaps, so the same flag was
		// fine there. With `-force_key_frames` already placing
		// keyframes at every `segmentLength*N` second, the default
		// HLS muxer behaviour (split only at keyframes) yields
		// clean, non-overlapping fragments.
		"-hls_flags", "temp_file",
		"-hls_segment_type", "fmp4",
		"-hls_playlist_type", "vod",
		"-hls_fmp4_init_filename", initFilename,
		"-hls_segment_filename", filepath.Join(outputDir, ".%v_%d.m4s"),
		"-var_stream_map", streamMap,
		filepath.Join(outputDir, "playlist_%v.m3u8"),
	)
	return args
}

type V3SegmentType struct {
	MimeType     string
	MakeFilename func(track Track, segment int) string
	ParseSegment func(str string) (int, error)
}

// V3SegmentTypeFMP4 — HLS media segments are fragmented MP4 (`.m4s`) plus a
// per-track `init_<track>.mp4` init segment that carries the `moov` box.
// Segment -1 is the URL convention for the init segment. The MIME served
// for both the init and media segments is `video/mp4` — browsers accept
// that for fMP4.
//
// fMP4 is the only segment type the streaming pipeline emits in v3:
// MPEG-TS (used by older HLS) and WebM-chunked (used by removed DASH)
// have both been removed in favour of fMP4 so MSE-based clients
// (@videojs/spf) and Safari's native HLS share a single segment format.
//
// Filenames carry a track prefix because ffmpeg's -var_stream_map demux
// produces independent video and audio variants — see Track / hlsSegmentArgs.
var V3SegmentTypeFMP4 = &V3SegmentType{
	MimeType: MimeMp4Video,
	MakeFilename: func(track Track, segment int) string {
		if segment == -1 {
			return fmt.Sprintf("init_%s.mp4", track)
		}
		return fmt.Sprintf("%s_%d.m4s", track, segment)
	},
	ParseSegment: func(str string) (int, error) {
		if str == "init" {
			return -1, nil
		}
		segment, err := strconv.Atoi(str)
		if err != nil || segment < 0 {
			err = ErrInvalidSegment
		}
		return segment, err
	},
}

type V3StreamOptions struct {
	StreamType *V3StreamType
	VideoFile  *models.VideoFile
	Resolution string
	Hash       string
	Track      Track
	Segment    string
}

type v3TranscodeProcess struct {
	cmd         *exec.Cmd
	context     context.Context
	cancel      context.CancelFunc
	cancelled   bool
	outputDir   string
	segmentType *V3SegmentType
	tracks      []Track
	// start is the segment index ffmpeg was invoked at (the
	// `-start_number`); used by ensureV3Transcode to detect requests that
	// fall before the start of the current run.
	start int
	// progress[track] is the highest segment index ffmpeg has finalised
	// on disk for that track (initialised to start-1, meaning "no
	// segments finalised yet"). Updated by checkSegments as the
	// un-suffixed segment files appear post-`+temp_file` rename.
	progress map[Track]int
	// suspended is true while the process is SIGSTOPped via
	// `suspendProcess`. Driven by `checkV3Transcode`'s lookahead-buffer
	// gate: when ffmpeg has produced significantly more content than
	// the client has requested (typical during a long pause), the
	// process is frozen in place to stop wasting CPU/GPU; when the
	// client consumes back down toward the head of the buffer, it's
	// resumed via `resumeProcess`. No restart between the two — the
	// same ffmpeg instance continues from exactly where it stopped,
	// so there's no AAC priming silence or PTS discontinuity.
	suspended bool
}

// Indirected so tests can substitute in-memory hooks instead of
// sending real signals to a real process.
var (
	suspendFn = suspendProcess
	resumeFn  = resumeProcess
)

// maxProgress returns the highest finalised segment index across all
// tracks. ensureV3Transcode uses this to detect requests that have run
// past the current ffmpeg run's working set + maxSegmentGap, which
// triggers a restart at the requested segment.
func (tp *v3TranscodeProcess) maxProgress() int {
	maxSegment := tp.start
	for _, t := range tp.tracks {
		if p, ok := tp.progress[t]; ok && p > maxSegment {
			maxSegment = p
		}
	}
	return maxSegment
}

type v3WaitingSegment struct {
	segmentType *V3SegmentType
	idx         int
	file        string
	path        string
	accessed    time.Time
	available   chan error
	done        atomic.Bool
}

type v3RunningStream struct {
	dir              string
	streamType       *V3StreamType
	vf               *models.VideoFile
	maxTranscodeSize int
	outputDir        string

	waitingSegments []*v3WaitingSegment
	tp              *v3TranscodeProcess
	lastAccessed    time.Time
	lastSegment     int

	displayRotationOnce sync.Once
	displayRotation     int64
}

func (t V3StreamType) String() string {
	return t.Name
}

func (t V3StreamType) FileDir(hash string, maxTranscodeSize int) string {
	if maxTranscodeSize == 0 {
		return fmt.Sprintf("%s_%s", hash, t)
	} else {
		return fmt.Sprintf("%s_%s_%d", hash, t, maxTranscodeSize)
	}
}

func v3HLSGetCodec(sm *StreamManager, name string) (codec VideoCodec) {
	switch name {
	case "hls":
		codec = VideoCodecLibX264
		if hwcodec := sm.encoder.hwCodecHLSCompatible(); hwcodec != nil && sm.config.GetTranscodeHardwareAcceleration() {
			codec = *hwcodec
		}
	case "hls-copy-fmp4", "hls-copy-fmp4-aac":
		codec = VideoCodecCopy
	}

	return codec
}

// videoOnly returns true when the source has no decodable audio. The
// streaming pipeline drops audio entirely (`-an` + a single video
// variant in -var_stream_map) in this case; the master playlist
// likewise omits the audio rendition.
func (s *v3RunningStream) videoOnly() bool {
	return ProbeAudioCodec(s.vf.AudioCodec) == MissingUnsupported
}

// tracks returns the set of HLS tracks ffmpeg will produce for this
// stream — always includes video; includes audio when the source has
// audio. Both are produced by a single ffmpeg process via -var_stream_map.
func (s *v3RunningStream) tracks() []Track {
	if s.videoOnly() {
		return []Track{TrackVideo}
	}
	return []Track{TrackVideo, TrackAudio}
}

func (s *v3RunningStream) getDisplayRotation(sm *StreamManager) int64 {
	s.displayRotationOnce.Do(func() {
		probe, err := sm.ffprobe.NewVideoFile(s.vf.Path)
		if err != nil {
			TranscodeDebugf("[transcode] could not probe source display rotation: %v", err)
			return
		}
		s.displayRotation = probe.Rotation
	})
	return s.displayRotation
}

func (s *v3RunningStream) makeStreamArgs(sm *StreamManager, segment int) Args {
	extraInputArgs := sm.config.GetLiveTranscodeInputArgs()
	extraOutputArgs := sm.config.GetLiveTranscodeOutputArgs()

	args := Args{"-hide_banner"}
	args = args.LogLevel(LogLevelError)

	codec := v3HLSGetCodec(sm, s.streamType.Name)

	// Rotated sources need an explicit GPU transpose because ffmpeg's generic
	// autorotation is a software filter. The full-hardware capability probe
	// includes that transpose; if the selected CUDA/QSV/VAAPI build or device
	// rejects it, this falls back to software decode/rotation while retaining
	// the configured hardware encoder.
	displayRotation := int64(0)
	if codec != VideoCodecCopy {
		displayRotation = s.getDisplayRotation(sm)
	}
	fullhw := codec.CodeName != "" &&
		sm.config.GetTranscodeHardwareAcceleration() &&
		sm.encoder.hwCanFullHWTranscodeWithRotation(sm.context, codec, s.vf, s.maxTranscodeSize, displayRotation)
	args = sm.encoder.hwDeviceInit(args, codec, fullhw)
	args = append(args, extraInputArgs...)

	frameRate := s.vf.FrameRateFinite()
	if segment > 0 {
		seekTime := float64(segment) * hlsSegmentDuration(frameRate)
		// `segDur` (not `segmentLength`) so the input seek lands at the
		// same scene-time the segment's `-output_ts_offset` declares.
		// On 59.94 fps sources segment 100 sits at 100·2.0020 = 200.20 s,
		// not 200.00 s — without using segDur, every restart would seek
		// the input to a slightly earlier point than the segment claims
		// to start at.
		args = args.Seek(seekTime)
	}

	args = displayRotationInputArgs(args, displayRotation, fullhw)
	args = args.Input(s.vf.Path)

	videoFilter := sm.encoder.hwMaxResFilter(codec, s.vf, s.maxTranscodeSize, fullhw)
	if fullhw {
		rotationFilter, _ := hardwareDisplayRotationFilter(codec, displayRotation)
		videoFilter = prependVideoFilter(rotationFilter, videoFilter)
	}

	args = append(args, s.streamType.Args(codec, segment, videoFilter, s.videoOnly(), s.outputDir, frameRate)...)

	args = append(args, extraOutputArgs...)

	return args
}

// checkSegments promotes ffmpeg's freshly-finalised dot-prefixed
// segment files (`.<track>_<idx>.m4s` and `.init_<track>.mp4`) to
// their public, un-prefixed names, and tracks per-track progress.
//
// The promotion is "first run wins": if the public name already
// exists (a previous ffmpeg run already produced the same segment
// index for the same track), the staged duplicate is discarded
// rather than overwriting. This keeps the bytes at a given segment
// index stable across the lifetime of the cache directory — without
// it, restarts would silently rewrite earlier segments with
// different AAC priming and PTS, leaving the player with a buffer
// patchwork of inconsistent timing.
//
// ffmpeg's `+temp_file` flag handles the per-write atomicity of
// each rename (each `.<name>.tmp` is renamed to `.<name>` only after
// the file is fully written and flushed), so the dot-prefixed file's
// presence is itself the "fully written" signal that this function
// keys on.
//
// Per-track loops because ffmpeg's HLS muxer closes per-track segment
// files asynchronously, so the two tracks finalise on independent
// schedules even though both come from one ffmpeg process.
func (tp *v3TranscodeProcess) checkSegments() {
	for _, track := range tp.tracks {
		for {
			next := tp.progress[track] + 1
			name := tp.segmentType.MakeFilename(track, next)
			staged := filepath.Join(tp.outputDir, "."+name)
			final := filepath.Join(tp.outputDir, name)
			if !segmentExists(staged) && !segmentExists(final) {
				break
			}
			if segmentExists(staged) {
				if !segmentExists(final) {
					_ = os.Rename(staged, final)
				} else {
					// Duplicate from a re-run; drop it rather than
					// overwriting the canonical bytes the player has
					// already seen.
					_ = os.Remove(staged)
				}
			}
			tp.progress[track] = next
		}

		// Publish init once the first media segment has landed.
		// Idempotent (no-op once the rename has happened).
		if tp.progress[track] >= tp.start {
			initStaged := filepath.Join(tp.outputDir, ".init_"+string(track)+".mp4")
			initFinal := filepath.Join(tp.outputDir, "init_"+string(track)+".mp4")
			if segmentExists(initStaged) && !segmentExists(initFinal) {
				_ = os.Rename(initStaged, initFinal)
			} else if segmentExists(initStaged) {
				_ = os.Remove(initStaged)
			}
		}
	}
}

func lastV3Segment(vf *models.VideoFile) int {
	segDur := hlsSegmentDuration(vf.FrameRateFinite())
	if segDur <= 0 {
		segDur = float64(segmentLength)
	}
	return int(math.Ceil(vf.Duration/segDur)) - 1
}

// parseEndQuery extracts an optional `?end=<seconds>` clip-end from the
// request. Returns (clamped value, true) when present and valid, else
// (0, false). A bare empty / unparseable / non-positive value reads as
// "not set" — the playlist generator treats this as no trim. Values >
// fileDuration are clamped to fileDuration so a stale or out-of-range
// value yields a clip ending at EOF rather than producing an empty
// playlist.
func parseEndQuery(r *http.Request, fileDuration float64) (float64, bool) {
	raw := r.URL.Query().Get("end")
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	if v > fileDuration {
		v = fileDuration
	}
	return v, true
}

// serveV3HLSManifestFMP4 serves a per-track HLS media playlist (video or audio).
// The track identity is embedded in the request path: e.g. `.../stream.m3u8/video.m3u8`.
// Stripping the `.m3u8` suffix gives the segment directory path, so init and
// media segment URLs resolve to `.../stream.m3u8/video/init.mp4` and
// `.../stream.m3u8/video/N.m4s` — matching the per-track segment routes.
func serveV3HLSManifestFMP4(sm *StreamManager, w http.ResponseWriter, r *http.Request, vf *models.VideoFile, resolution string) {
	if sm.cacheDir == "" {
		logger.Error("[transcode] cannot live transcode with HLS because cache dir is unset")
		http.Error(w, "cannot live transcode with HLS because cache dir is unset", http.StatusServiceUnavailable)
		return
	}

	probeResult, err := sm.ffprobe.NewVideoFile(vf.Path)
	if err != nil {
		logger.Warnf("[transcode] error generating HLS manifest: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	prefix := r.Header.Get("X-Forwarded-Prefix")

	baseUrl := *r.URL
	baseUrl.RawQuery = ""
	// Strip the ".m3u8" suffix so the segment base is the track directory:
	// ".../stream.m3u8/video.m3u8" → ".../stream.m3u8/video", giving init
	// URL ".../stream.m3u8/video/init.mp4" and segments ".../stream.m3u8/video/N.m4s".
	baseURL := strings.TrimSuffix(prefix+baseUrl.String(), ".m3u8")

	urlQuery := url.Values{}

	copyAuthParams(urlQuery, r.URL.Query())

	if resolution != "" {
		urlQuery.Set(resolutionParamKey, resolution)
	}

	urlQueryString := ""
	if len(urlQuery) > 0 {
		urlQueryString = "?" + urlQuery.Encode()
	}

	// `segDur` is the real per-segment duration computed from the
	// source's frame rate. For integer fps it's exactly `segmentLength`;
	// for NTSC fractionals (59.94 fps etc.) it drifts upward by a few
	// milliseconds. Use it for the `EXT-X-TARGETDURATION` ceiling, the
	// `EXTINF` per-segment line, and the leftover-duration walk so every
	// value matches the bytes ffmpeg actually emits. See
	// `hlsSegmentDuration` for the math.
	frameRate := probeResult.FrameRate
	segDur := hlsSegmentDuration(frameRate)

	// Playlist trimming is gated on whether `?end=` is present in the
	// URL:
	//
	//   - `?end=` absent (typical scene playback): emit every segment
	//     from 0 to EOF, mediaSequence=0. The client's hls.js gets
	//     `config.startPosition` from the `?start=` value so it starts
	//     loading at the right segment instead of segment 0 (no
	//     cold-start detour). Native iOS fullscreen sees the full
	//     scene as seekable and labels its slider "0:00 → sceneDur."
	//
	//   - `?end=` present (marker / clip playback): trim to
	//     `[⌊start/segDur⌋, ⌈end/segDur⌉-1]` with mediaSequence set to
	//     the first segment. hls.js rebases the MSE timeline to start
	//     at 0; native iOS fullscreen sees only the clip as seekable
	//     and labels its slider "0:00 → clipDur."
	//
	// Either way, `?start=` flows through into per-segment URLs via
	// `urlQueryString` below; the server uses no further information
	// from it. The client controls hls.js's start segment, and the
	// server's first-segment-request triggers `startV3Transcode` at
	// whatever segment hls.js asks for.
	startTime, _ := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	endTime, hasEnd := parseEndQuery(r, probeResult.FileDuration)

	startSegment := 0
	endSegment := -1 // -1 == "to EOF"; updated below when ?end= is set
	mediaSequence := 0
	playlistStart := 0.0
	playlistEnd := probeResult.FileDuration

	if hasEnd {
		// Clamp start to valid range; treat invalid/missing as 0.
		if startTime < 0 || startTime >= probeResult.FileDuration {
			startTime = 0
		}
		if endTime <= startTime {
			endTime = probeResult.FileDuration
		}
		startSegment = int(math.Floor(startTime / segDur))
		// Last segment to include: the segment that *contains* `endTime`.
		// floor((endTime - epsilon) / segDur) excludes a segment that
		// starts exactly at endTime (it contains no clip content).
		lastIdx := int(math.Floor((endTime - 1e-6) / segDur))
		if lastIdx < startSegment {
			lastIdx = startSegment
		}
		endSegment = lastIdx
		mediaSequence = startSegment
		playlistStart = float64(startSegment) * segDur
		playlistEnd = math.Min(float64(endSegment+1)*segDur, probeResult.FileDuration)
	}

	var buf bytes.Buffer

	fmt.Fprint(&buf, "#EXTM3U\n")
	fmt.Fprint(&buf, "#EXT-X-VERSION:7\n")
	fmt.Fprintf(&buf, "#EXT-X-MEDIA-SEQUENCE:%d\n", mediaSequence)
	// EXT-X-TARGETDURATION must be the integer ceiling of the longest
	// segment's duration. With segDur ≤ 2.01 s for any realistic frame
	// rate, ceil → 3, but flooring to `int(segDur)+1` for the safe
	// rounding path keeps the value well above the actual segment.
	fmt.Fprintf(&buf, "#EXT-X-TARGETDURATION:%d\n", int(math.Ceil(segDur)))
	fmt.Fprint(&buf, "#EXT-X-PLAYLIST-TYPE:VOD\n")
	// Single init segment for the whole playlist; required for fMP4 HLS.
	fmt.Fprintf(&buf, "#EXT-X-MAP:URI=\"%s/init.mp4%s\"\n", baseURL, urlQueryString)

	leftover := playlistEnd - playlistStart
	segment := startSegment

	for leftover > 0 && (endSegment < 0 || segment <= endSegment) {
		thisLength := segDur
		if leftover < thisLength {
			thisLength = leftover
		}

		fmt.Fprintf(&buf, "#EXTINF:%f,\n", thisLength)
		fmt.Fprintf(&buf, "%s/%d.m4s%s\n", baseURL, segment, urlQueryString)

		leftover -= thisLength
		segment++
	}

	fmt.Fprint(&buf, "#EXT-X-ENDLIST\n")

	w.Header().Set("Content-Type", MimeHLS)
	utils.ServeStaticContent(w, r, buf.Bytes())
}

// ── Master (multivariant) playlists ──────────────────────────────────────────
//
// HLS clients are expected to enter a presentation through a multivariant
// (master) playlist that advertises one or more variants via
// `EXT-X-STREAM-INF`. Stash only ever has one variant per stream type
// (transcode-or-copy + the chosen resolution), but exposing it through a
// master playlist matters for two reasons:
//
//  1. MSE-based players (e.g. @videojs/spf) refuse to enter a presentation
//     at the media-playlist level — they need codec/resolution metadata
//     advertised in the master to size SourceBuffers before fetching
//     segments. hls.js was lenient about this; SPF and others aren't.
//  2. Even Safari's native HLS handles a single-variant master correctly,
//     so the master path works for every client.
//
// The variant URL is the same path minus `.master` (e.g. master at
// `/stream.master.m3u8` references `stream.m3u8`), with all other query
// parameters preserved so `?start=`, `?resolution=`, and `?apikey=` flow
// through to the variant.

// fmp4MasterCodecs returns the RFC 6381 codecs string for the source's
// video and audio tracks. MSE creates SourceBuffers with the codec
// declared in the master playlist, so advertised codecs must match what
// the codec-copy variant actually emits — otherwise appended segments
// fail to decode silently. Probes the source for profile + level so the
// avc1/hvc1/av01 strings reflect the real bitstream.
func fmp4MasterCodecs(sm *StreamManager, vf *models.VideoFile) string {
	probe, _ := sm.ffprobe.NewVideoFile(vf.Path)
	video := videoCodecString(vf.VideoCodec, probe)
	audio := mapAudioCodecToRFC6381(vf.AudioCodec)
	switch {
	case video != "" && audio != "":
		return video + "," + audio
	case video != "":
		return video
	case audio != "":
		return audio
	default:
		// Last-resort fallback: a permissive Baseline 3.0 + AAC LC. The
		// client still runs `canPlayType` against this string before
		// picking the variant, so an inaccurate fallback fails the
		// source filter rather than producing silent decode errors.
		return "avc1.42E01E,mp4a.40.2"
	}
}

// videoCodecString builds the RFC 6381 codec string for the source's
// video track using the actual probed profile/level when available.
// Falls back to permissive defaults for codecs we don't have probe
// fields for.
func videoCodecString(codec string, probe *VideoFile) string {
	var profile string
	var level int
	if probe != nil && probe.VideoStream != nil {
		profile = probe.VideoStream.Profile
		level = probe.VideoStream.Level
	}
	switch codec {
	case H264:
		return formatAvcCodec(profile, level)
	case H265, Hevc:
		// HEVC RFC 6381 form is `hvc1.<gen-prof>.<prof-comp>.<tier-level>.<constr>`
		// — encoding all five fields requires the GeneralProfileSpace,
		// CompatibilityFlags, Tier, and ConstraintFlags. ffprobe doesn't
		// surface those directly, so use a conservative Main@3.1 string.
		// Browsers that support HEVC at all generally accept this baseline
		// codec hint and decode whatever's actually in the stream.
		return "hvc1.1.6.L93.B0"
	case "av1":
		// AV1 codec string format: av01.<profile>.<level><tier>.<bitdepth>
		// Default to Main Profile, Level 4.0, Main Tier, 8-bit. Real-world
		// AV1 sources rarely exceed level 5.1, and AV1-decoding browsers
		// (Safari 17+, Chrome, Firefox) tolerate level mismatches as long
		// as the actual content fits within their decoder limits.
		return "av01.0.04M.08"
	case Vp9:
		return "vp09.00.10.08"
	default:
		return ""
	}
}

// formatAvcCodec returns the H.264 RFC 6381 codec string `avc1.PPCCLL`
// where PP is the profile_idc, CC is the profile_compat (constraint
// flags), and LL is the level_idc, all in lowercase hex. profile is the
// ffprobe-reported string ("High", "Main", "Baseline", …), level is
// level*10 (40 = Level 4.0). Falls back to High@4.0 when probe data is
// missing — that matches what the H.264 transcode pipeline forces.
func formatAvcCodec(profile string, level int) string {
	var profileIDC int
	switch strings.ToLower(profile) {
	case "baseline", "constrained baseline":
		profileIDC = 0x42
	case "main":
		profileIDC = 0x4D
	case "high":
		profileIDC = 0x64
	case "high 10":
		profileIDC = 0x6E
	case "high 4:2:2":
		profileIDC = 0x7A
	case "high 4:4:4 predictive":
		profileIDC = 0xF4
	default:
		profileIDC = 0x64 // High — safe modern default
	}
	if level <= 0 {
		level = 40 // Level 4.0
	}
	return fmt.Sprintf("avc1.%02x00%02x", profileIDC, level)
}

func mapAudioCodecToRFC6381(codec string) string {
	switch ProbeAudioCodec(codec) {
	case Aac:
		return "mp4a.40.2" // AAC-LC
	case Opus:
		return "opus"
	case Mp3:
		return "mp4a.40.34"
	default:
		return ""
	}
}

// approxBandwidth returns a bits-per-second estimate for the master
// playlist's `BANDWIDTH=` attribute. The HLS spec requires this to be the
// peak segment bitrate; for our purposes a rough average suffices and any
// non-zero value satisfies clients. Falls back to 2 Mbps for sources with
// no probe data.
func approxBandwidth(vf *models.VideoFile) int {
	if vf.BitRate > 0 {
		return int(vf.BitRate)
	}
	return 2_000_000
}

// scaledResolution scales the source's width × height by the
// `?resolution=` query value, returning the dimensions that should be
// advertised in the master playlist's RESOLUTION attribute. Returns the
// source dimensions unchanged when no resolution override is in effect.
func scaledResolution(vf *models.VideoFile, resolution string) (int, int) {
	w, h := vf.Width, vf.Height
	if resolution == "" {
		return w, h
	}
	maxResolution := models.StreamingResolutionEnum(resolution).GetMaxResolution()
	if maxResolution == 0 || w == 0 || h == 0 {
		return w, h
	}
	short := h
	if w < short {
		short = w
	}
	if maxResolution >= short {
		return w, h
	}
	scale := float64(maxResolution) / float64(short)
	return int(float64(w) * scale), int(float64(h) * scale)
}

// writeMasterPlaylist emits a HLS multivariant (master) playlist body to buf.
// videoURL is the video track's media playlist URL (always present). audioURL
// is the audio rendition's playlist URL; when non-empty an EXT-X-MEDIA audio
// rendition is added and the video variant's EXT-X-STREAM-INF carries AUDIO="aac".
func writeMasterPlaylist(buf *bytes.Buffer, videoURL, audioURL string, bandwidth int, width, height int, codecs string) {
	fmt.Fprint(buf, "#EXTM3U\n")
	fmt.Fprint(buf, "#EXT-X-VERSION:7\n")
	fmt.Fprint(buf, "#EXT-X-INDEPENDENT-SEGMENTS\n")
	if audioURL != "" {
		fmt.Fprintf(buf, "#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"aac\",NAME=\"Audio\",DEFAULT=YES,AUTOSELECT=YES,URI=\"%s\"\n", audioURL)
	}
	fmt.Fprintf(buf, "#EXT-X-STREAM-INF:BANDWIDTH=%d", bandwidth)
	if width > 0 && height > 0 {
		fmt.Fprintf(buf, ",RESOLUTION=%dx%d", width, height)
	}
	if codecs != "" {
		fmt.Fprintf(buf, ",CODECS=\"%s\"", codecs)
	}
	if audioURL != "" {
		fmt.Fprintf(buf, ",AUDIO=\"aac\"")
	}
	fmt.Fprintln(buf)
	fmt.Fprintln(buf, videoURL)
}

// hlsMasterTrackURL builds the per-track media playlist URL referenced from
// a multivariant master. Collapses `.master.` to `.` and appends
// `/<track>.m3u8`, preserving all original query params so `?start=`,
// `?resolution=`, and `?apikey=` flow through to each track playlist.
//
// Examples:
//
//	/stream.master.m3u8      + TrackVideo → /stream.m3u8/video.m3u8
//	/stream.fmp4.master.m3u8 + TrackAudio → /stream.fmp4.m3u8/audio.m3u8
func hlsMasterTrackURL(r *http.Request, track Track) string {
	u := *r.URL
	prefix := r.Header.Get("X-Forwarded-Prefix")
	u.Path = strings.Replace(u.Path, ".master.", ".", 1) + "/" + string(track) + ".m3u8"
	return prefix + u.String()
}

// serveV3HLSMasterManifest serves the master playlist for the H.264+AAC
// transcode variant (`/stream.m3u8`). Codecs match the pinned
// `-profile:v high -level 5.1` in the encoder args (CodecInit for
// VideoCodecLibX264) so the SourceBuffer SPF creates accepts the
// segments the encoder produces. The pinned ceiling is high enough
// for 4K@60 — the encoder picks lower actual levels for typical
// content, and MSE (lenient on Chrome, stricter on Safari) accepts
// segments whose level is at or below the declared ceiling.
//
// Audio is omitted from the CODECS string when the source has no
// audio. The transcode pipeline emits video-only segments in that
// case (`-an` in `hlsSegmentArgs`); declaring an AAC track that
// never arrives makes MSE allocate an audio SourceBuffer that stays
// unfed, blocking the readyState transition that gates playback.
// Symptom on the affected files (e.g. AV1 MKV with no audio): the
// player loads segments, hls.js logs "no audio data", and playback
// never starts. iOS Safari's native HLS path fails the same way.
func serveV3HLSMasterManifest(sm *StreamManager, w http.ResponseWriter, r *http.Request, vf *models.VideoFile, resolution string) {
	width, height := scaledResolution(vf, resolution)
	videoURL := hlsMasterTrackURL(r, TrackVideo)
	var audioURL string
	codecs := "avc1.640033" // H.264 High @ Level 5.1
	if ProbeAudioCodec(vf.AudioCodec) != MissingUnsupported {
		audioURL = hlsMasterTrackURL(r, TrackAudio)
		codecs += ",mp4a.40.2" // AAC-LC
	}
	var buf bytes.Buffer
	writeMasterPlaylist(
		&buf,
		videoURL, audioURL,
		approxBandwidth(vf),
		width, height,
		codecs,
	)
	w.Header().Set("Content-Type", MimeHLS)
	utils.ServeStaticContent(w, r, buf.Bytes())
}

// serveV3HLSMasterManifestFMP4 serves the master playlist for the codec-copy
// fMP4 variant (`/stream.fmp4.m3u8`). Codecs come from the source's
// probe data so the advertised codec string matches the actual init
// segment — MSE rejects mismatched segments silently.
func serveV3HLSMasterManifestFMP4(sm *StreamManager, w http.ResponseWriter, r *http.Request, vf *models.VideoFile, resolution string) {
	width, height := scaledResolution(vf, resolution)
	videoURL := hlsMasterTrackURL(r, TrackVideo)
	var audioURL string
	if ProbeAudioCodec(vf.AudioCodec) != MissingUnsupported {
		audioURL = hlsMasterTrackURL(r, TrackAudio)
	}
	var buf bytes.Buffer
	writeMasterPlaylist(
		&buf,
		videoURL, audioURL,
		approxBandwidth(vf),
		width, height,
		fmp4MasterCodecs(sm, vf),
	)
	w.Header().Set("Content-Type", MimeHLS)
	utils.ServeStaticContent(w, r, buf.Bytes())
}

// serveV3HLSMasterManifestFMP4AAC serves the master playlist for the
// video-copy + AAC-transcode fMP4 variant (`/stream.fmp4.aac.m3u8`). The
// video codec string is taken from the source's probe data (the bitstream
// is byte-identical to source); the audio codec is hard-coded to AAC LC
// since the variant always re-encodes audio to AAC.
func serveV3HLSMasterManifestFMP4AAC(sm *StreamManager, w http.ResponseWriter, r *http.Request, vf *models.VideoFile, resolution string) {
	width, height := scaledResolution(vf, resolution)
	videoURL := hlsMasterTrackURL(r, TrackVideo)
	var audioURL string
	if ProbeAudioCodec(vf.AudioCodec) != MissingUnsupported {
		audioURL = hlsMasterTrackURL(r, TrackAudio)
	}
	probe, _ := sm.ffprobe.NewVideoFile(vf.Path)
	video := videoCodecString(vf.VideoCodec, probe)
	codecs := video
	if codecs == "" {
		// Fallback: same permissive default fmp4MasterCodecs uses.
		codecs = "avc1.42E01E"
	}
	if audioURL != "" {
		codecs += ",mp4a.40.2"
	}
	var buf bytes.Buffer
	writeMasterPlaylist(
		&buf,
		videoURL, audioURL,
		approxBandwidth(vf),
		width, height,
		codecs,
	)
	w.Header().Set("Content-Type", MimeHLS)
	utils.ServeStaticContent(w, r, buf.Bytes())
}

func (sm *StreamManager) ServeV3Manifest(w http.ResponseWriter, r *http.Request, streamType *V3StreamType, vf *models.VideoFile, resolution string) {
	switch streamType.Name {
	case "hls", "hls-copy-fmp4", "hls-copy-fmp4-aac":
		serveV3HLSManifestFMP4(sm, w, r, vf, resolution)
	default:
		http.Error(w, "no manifest for stream type "+streamType.Name, http.StatusNotFound)
	}
}

// ServeV3MasterManifest serves a master playlist for the given stream type.
// Dispatched on the stream type's name rather than a per-StreamType field
// so the StreamType definitions stay focused on segment generation.
func (sm *StreamManager) ServeV3MasterManifest(w http.ResponseWriter, r *http.Request, streamType *V3StreamType, vf *models.VideoFile, resolution string) {
	switch streamType.Name {
	case "hls":
		serveV3HLSMasterManifest(sm, w, r, vf, resolution)
	case "hls-copy-fmp4":
		serveV3HLSMasterManifestFMP4(sm, w, r, vf, resolution)
	case "hls-copy-fmp4-aac":
		serveV3HLSMasterManifestFMP4AAC(sm, w, r, vf, resolution)
	default:
		http.Error(w, "no master playlist for stream type "+streamType.Name, http.StatusNotFound)
	}
}

func (sm *StreamManager) serveV3WaitingSegment(w http.ResponseWriter, r *http.Request, segment *v3WaitingSegment) {
	select {
	case <-r.Context().Done():
		break
	case err := <-segment.available:
		if err == nil {
			TranscodeTracef("[transcode] streaming segment file %s", segment.file)
			w.Header().Set("Content-Type", segment.segmentType.MimeType)
			// Once a segment file is written it is never replaced —
			// `checkSegments` keeps the first generated copy and discards
			// later ones — so the bytes at a given segment index are
			// stable for the lifetime of the cache directory. Mark them
			// `immutable` so Safari's native HLS pipeline keeps the
			// downloaded segments in its buffer instead of re-fetching
			// them after every pause/resume cycle. The default
			// `no-cache` from `utils.ServeStaticFile` defeats Safari's
			// HLS cache and produces visible re-buffer pauses.
			w.Header().Set("Cache-Control", "private, max-age=3600, immutable")
			http.ServeFile(w, r, segment.path)
		} else if !errors.Is(err, context.Canceled) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
	segment.done.Store(true)
}

func (sm *StreamManager) ServeV3Segment(w http.ResponseWriter, r *http.Request, options V3StreamOptions) {
	if sm.cacheDir == "" {
		logger.Error("[transcode] cannot live transcode files because cache dir is unset")
		http.Error(w, "cannot live transcode files because cache dir is unset", http.StatusServiceUnavailable)
		return
	}

	if options.Hash == "" {
		http.Error(w, "invalid hash", http.StatusBadRequest)
		return
	}

	streamType := options.StreamType

	segment, err := streamType.SegmentType.ParseSegment(options.Segment)
	maxSegment := lastV3Segment(options.VideoFile)
	// error if segment is past the end of the video
	if err != nil || segment > maxSegment {
		http.Error(w, "invalid segment", http.StatusBadRequest)
		return
	}

	maxTranscodeSize := sm.config.GetMaxStreamingTranscodeSize().GetMaxResolution()
	if options.Resolution != "" {
		maxTranscodeSize = models.StreamingResolutionEnum(options.Resolution).GetMaxResolution()
	}

	dir := options.StreamType.FileDir(options.Hash, maxTranscodeSize)
	outputDir := filepath.Join(sm.cacheDir, dir)

	name := streamType.SegmentType.MakeFilename(options.Track, segment)
	file := filepath.Join(dir, name)

	sm.streamsMutex.Lock()

	stream := sm.v3RunningStreams[dir]
	if stream == nil {
		// New stream for this (hash, resolution, codec) combination.
		// Eagerly tear down any sibling streams for the SAME source
		// scene that haven't been accessed recently — quality swaps
		// (e.g. STANDARD → STANDARD_HD) and engine swaps
		// (direct ↔ HLS) leave the previous transcode running until
		// `v3MaxIdleTime` (60 s) elapses, unnecessarily holding ffmpeg
		// + GPU resources visible in `intel-gpu-top`. Killing siblings
		// here collapses that window to ~0.
		//
		// The 2 s grace window protects genuinely concurrent
		// multi-client streams (two browser tabs watching the same
		// scene at different qualities — both have recent lastAccessed
		// timestamps) from being torn down by an unrelated single
		// client's swap. The most-recent-segment-request wins:
		// whichever stream just made a request keeps its sibling
		// alive for at least 2 s of its own idleness.
		newFileID := options.VideoFile.ID
		cutoff := time.Now().Add(-2 * time.Second)
		for siblingDir, sibling := range sm.v3RunningStreams {
			if siblingDir == dir || sibling.vf == nil {
				continue
			}
			if sibling.vf.ID != newFileID {
				continue
			}
			if sibling.lastAccessed.After(cutoff) {
				continue
			}
			TranscodeDebugf(
				"[transcode] tearing down sibling stream %s (new sibling %s starting; last access %v ago)",
				siblingDir, dir, time.Since(sibling.lastAccessed),
			)
			sm.stopV3Transcode(sibling)
			sm.removeV3TranscodeFiles(sibling)
			delete(sm.v3RunningStreams, siblingDir)
		}

		// Wipe the on-disk cache for this stream before creating it.
		// The idempotent-rename invariant in `checkSegments` is per-session
		// — leftover segments from a previous session (e.g. server restart
		// or `v3MaxIdleTime` cleanup that didn't run because of a crash)
		// will defeat the buffer-full check in `checkV3Transcode`, which
		// would see them as already-produced and stop ffmpeg after one
		// segment. The resulting buffer is a patchwork of segments from
		// different ffmpeg runs with mismatched AAC priming / PTS, and
		// the player stalls at the first cross-run boundary.
		if err := os.RemoveAll(outputDir); err != nil {
			TranscodeDebugf("[transcode] error clearing stale cache for %s: %v", dir, err)
		}
		stream = &v3RunningStream{
			dir:              dir,
			streamType:       options.StreamType,
			vf:               options.VideoFile,
			maxTranscodeSize: maxTranscodeSize,
			outputDir:        outputDir,

			// initialize to cap 10 to avoid reallocations
			waitingSegments: make([]*v3WaitingSegment, 0, 10),
		}
		sm.v3RunningStreams[dir] = stream
	}

	now := time.Now()
	stream.lastAccessed = now
	if segment != -1 {
		stream.lastSegment = segment
	}

	v3WaitingSegment := &v3WaitingSegment{
		segmentType: streamType.SegmentType,
		idx:         segment,
		file:        file,
		path:        filepath.Join(sm.cacheDir, file),
		accessed:    now,
		available:   make(chan error, 1),
	}
	stream.waitingSegments = append(stream.waitingSegments, v3WaitingSegment)

	sm.streamsMutex.Unlock()

	sm.serveV3WaitingSegment(w, r, v3WaitingSegment)
}

// assume lock is held
func (sm *StreamManager) startV3Transcode(stream *v3RunningStream, segment int, done chan<- error) {
	// generate segment 0 if init segment requested
	if segment == -1 {
		segment = 0
	}

	TranscodeDebugf("[transcode] starting transcode for %s at segment #%d", stream.dir, segment)

	if err := os.MkdirAll(stream.outputDir, os.ModePerm); err != nil {
		logger.Errorf("[transcode] %v", err)
		done <- err
		return
	}

	lockCtx := sm.lockManager.ReadLock(sm.context, stream.vf.Path)

	args := stream.makeStreamArgs(sm, segment)
	cmd := sm.encoder.Command(lockCtx, args)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		logger.Errorf("[transcode] ffmpeg stderr not available: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if nil != err {
		logger.Errorf("[transcode] ffmpeg stdout not available: %v", err)
	}

	TranscodeTracef("[transcode] running %s", cmd)
	if err := cmd.Start(); err != nil {
		lockCtx.Cancel()
		err = fmt.Errorf("error starting transcode process: %w", err)
		logger.Errorf("[transcode] %v", err)
		done <- err
		return
	}

	tracks := stream.tracks()
	progress := make(map[Track]int, len(tracks))
	for _, t := range tracks {
		// "no segments finalised yet"; checkSegments advances this as
		// each `<track>_<idx>.m4s` rename lands on disk.
		progress[t] = segment - 1
	}
	tp := &v3TranscodeProcess{
		cmd:         cmd,
		context:     lockCtx,
		cancel:      lockCtx.Cancel,
		outputDir:   stream.outputDir,
		segmentType: stream.streamType.SegmentType,
		tracks:      tracks,
		start:       segment,
		progress:    progress,
	}
	stream.tp = tp

	go func() {
		errStr, _ := io.ReadAll(stderr)
		outStr, _ := io.ReadAll(stdout)

		errCmd := cmd.Wait()

		var err error

		// don't log error if cancelled
		if !tp.cancelled {
			e := string(errStr)
			if e == "" {
				e = string(outStr)
			}
			if e != "" {
				err = errors.New(e)
			} else {
				err = errCmd
			}

			if err != nil {
				err = fmt.Errorf("ffmpeg error when running command <%s>: %w", strings.Join(cmd.Args, " "), err)

				var exitError *exec.ExitError
				if !errors.As(err, &exitError) {
					logger.Errorf("[transcode] %v", err)
				}
			}
		}

		sm.streamsMutex.Lock()

		// make sure that cancel is called to prevent memory leaks
		tp.cancel()

		// clear remaining segments after ffmpeg exit
		tp.checkSegments()

		if stream.tp == tp {
			stream.tp = nil
		}

		sm.streamsMutex.Unlock()

		if err != nil {
			done <- err
		}
	}()
}

// assume lock is held
func (sm *StreamManager) stopV3Transcode(stream *v3RunningStream) {
	tp := stream.tp
	if tp != nil {
		// If suspended, resume first. The context-cancel path below
		// will eventually SIGKILL via os/exec which bypasses SIGSTOP,
		// so this isn't strictly required — but SIGCONT lets any
		// queued cleanup signals (including a future SIGTERM) deliver
		// promptly, and means the process exits its normal way rather
		// than via -9. Cheap insurance.
		if tp.suspended {
			_ = resumeFn(tp.cmd.Process)
			tp.suspended = false
		}
		tp.cancel()
		tp.cancelled = true
	}
}

func (sm *StreamManager) checkV3Transcode(stream *v3RunningStream, now time.Time) {
	if len(stream.waitingSegments) == 0 && stream.lastAccessed.Add(v3MaxIdleTime).Before(now) {
		// Stream expired. Cancel the transcode process and delete the files
		TranscodeDebugf("[transcode] stream for %s not accessed recently. Cancelling transcode and removing files", stream.dir)

		sm.stopV3Transcode(stream)
		sm.removeV3TranscodeFiles(stream)

		delete(sm.v3RunningStreams, stream.dir)
		return
	}

	// Supersession kill: if a sibling stream for the SAME file has been
	// accessed significantly more recently than this one, the client has
	// almost certainly swapped resolution/codec and this stream is
	// orphaned. The `ServeV3Segment` sibling-kill only fires on new-stream
	// creation and is gated by a 2 s grace window, which lets a stream
	// linger until `v3MaxIdleTime` (60 s) when a swap happens before the
	// previous stream's `lastAccessed` goes stale. Here we re-evaluate
	// every monitor tick, so the orphan is reaped within a few seconds.
	//
	// `supersededIdle` is the cutoff: if this stream's `lastAccessed`
	// trails any sibling's by more than this, we treat it as superseded.
	// Chosen as 5 s to comfortably outrun typical client-side swap
	// latency (URL change → new <video> → playlist + first segment fetch)
	// while still cleaning up well inside the 60 s `v3MaxIdleTime` window.
	const supersededIdle = 5 * time.Second
	if stream.vf != nil && len(stream.waitingSegments) == 0 {
		fileID := stream.vf.ID
		for siblingDir, sibling := range sm.v3RunningStreams {
			if siblingDir == stream.dir || sibling.vf == nil {
				continue
			}
			if sibling.vf.ID != fileID {
				continue
			}
			if sibling.lastAccessed.After(stream.lastAccessed.Add(supersededIdle)) {
				TranscodeDebugf(
					"[transcode] superseded stream %s (sibling %s accessed %v ago, this %v ago)",
					stream.dir, siblingDir,
					time.Since(sibling.lastAccessed),
					time.Since(stream.lastAccessed),
				)
				sm.stopV3Transcode(stream)
				sm.removeV3TranscodeFiles(stream)
				delete(sm.v3RunningStreams, stream.dir)
				return
			}
		}
	}

	// Lookahead-buffer gate. We can't stop+restart ffmpeg when the
	// buffer fills (every restart introduces a fresh AAC encoder
	// priming silence at the run's first segment — see commit history
	// for the old `maxSegmentBuffer=15` removal), but we CAN freeze
	// the process in place with SIGSTOP. The same ffmpeg instance
	// stays alive holding its encoder state intact; when the client
	// drains the buffer back down to `v3ResumeLookaheadSegments`, we
	// SIGCONT and it continues from exactly where it left off. No
	// restart, no audio discontinuity.
	//
	// The `v3MaxIdleTime` idle cleanup above still catches the genuine
	// "abandoned stream" case (user closed the tab, keepalive
	// stopped) — that path SIGKILLs via the context, which bypasses
	// SIGSTOP. So suspended ffmpegs don't leak; they only persist as
	// long as the player is actually mounted.
	//
	// Skip the gate when there's a segment request in flight — the
	// client is actively asking, so producing more is by definition
	// not wasted. Also resume any stream where the client has caught
	// up; otherwise a paused stream that the user navigates back to
	// would stay frozen indefinitely.
	if stream.tp != nil {
		tp := stream.tp
		ahead := tp.maxProgress() - stream.lastSegment
		hasWaiting := len(stream.waitingSegments) > 0
		switch {
		case hasWaiting && tp.suspended:
			// Client is asking; thaw immediately regardless of
			// lookahead — the waiting segment may sit past the current
			// producable range.
			if err := resumeFn(tp.cmd.Process); err != nil {
				TranscodeDebugf("[transcode] resume (request) %s: %v", stream.dir, err)
			} else {
				TranscodeDebugf("[transcode] resumed %s (segment requested)", stream.dir)
			}
			tp.suspended = false
		case !tp.suspended && !hasWaiting && ahead >= v3SuspendLookaheadSegments:
			if err := suspendFn(tp.cmd.Process); err != nil {
				TranscodeDebugf("[transcode] suspend %s: %v", stream.dir, err)
			} else {
				TranscodeDebugf("[transcode] suspended %s (%d segments ahead of client)", stream.dir, ahead)
				tp.suspended = true
			}
		case tp.suspended && ahead <= v3ResumeLookaheadSegments:
			if err := resumeFn(tp.cmd.Process); err != nil {
				TranscodeDebugf("[transcode] resume %s: %v", stream.dir, err)
			} else {
				TranscodeDebugf("[transcode] resumed %s (client drained to %d segments ahead)", stream.dir, ahead)
				tp.suspended = false
			}
		}
	}
}

func (s *v3WaitingSegment) checkAvailable(now time.Time) bool {
	if segmentExists(s.path) {
		s.available <- nil
		return true
	} else if s.accessed.Add(maxSegmentWait).Before(now) {
		err := fmt.Errorf("timed out waiting for segment file %s to be generated", s.file)
		logger.Errorf("[transcode] %v", err)
		s.available <- err
		return true
	}
	return false
}

// ensureV3Transcode will start a new transcode process if the transcode
// is more than maxSegmentGap behind the requested segment
func (sm *StreamManager) ensureV3Transcode(stream *v3RunningStream, segment *v3WaitingSegment) bool {
	segmentIdx := segment.idx
	tp := stream.tp
	if tp == nil {
		sm.startV3Transcode(stream, segmentIdx, segment.available)
		return true
	}
	// Init segment requests use idx == -1. When a transcode is already
	// running, the init file will be published by checkSegments' rename
	// once the first media segment lands; don't kill ffmpeg over it.
	// Each restart produces segments with different AAC priming and
	// encoder reset state, leaving inconsistent PTS on disk.
	if segmentIdx == -1 {
		return false
	}
	if segmentIdx < tp.start || tp.maxProgress()+maxSegmentGap < segmentIdx {
		// only stop the transcode process here - it will be restarted only
		// after the old process exits as stream.tp will then be nil.
		sm.stopV3Transcode(stream)
		return true
	}
	return false
}

// runs every monitorInterval
func (sm *StreamManager) monitorV3Streams() {
	sm.streamsMutex.Lock()
	defer sm.streamsMutex.Unlock()

	now := time.Now()

	for _, stream := range sm.v3RunningStreams {
		if stream.tp != nil {
			stream.tp.checkSegments()
		}

		transcodeStarted := false
		temp := stream.waitingSegments[:0]
		for _, segment := range stream.waitingSegments {
			remove := false
			if segment.done.Load() || segment.checkAvailable(now) {
				remove = true
			} else if !transcodeStarted {
				transcodeStarted = sm.ensureV3Transcode(stream, segment)
			}
			if !remove {
				temp = append(temp, segment)
			}
		}
		stream.waitingSegments = temp

		if !transcodeStarted {
			sm.checkV3Transcode(stream, now)
		}
	}
}

// assume lock is held
func (sm *StreamManager) removeV3TranscodeFiles(stream *v3RunningStream) {
	path := stream.outputDir
	if err := os.RemoveAll(path); err != nil {
		logger.Warnf("[transcode] error removing segment directory %s: %v", path, err)
	}
}

// StopV3StreamsForFile tears down every `v3RunningStream` for the given
// video file, except optionally one whose dir matches `exceptDir`.
// Called from the explicit "stop streaming" endpoint that the v3
// frontend fires via `navigator.sendBeacon` on:
//
//   - HLS → direct stream swap (sibling-kill in `ServeV3Segment` doesn't
//     fire because direct stream never hits that path; `exceptDir`
//     empty, kills all).
//   - HLS → HLS swap (different streamType / resolution; `exceptDir`
//     is the new stream's dir so the freshly-starting transcode
//     survives, only the previous-resolution stream is reaped).
//   - Player unmount (tab close, page navigation; `exceptDir` empty).
//
// Without this, the previous HLS transcode lingers for `v3MaxIdleTime`
// (60 s) holding ffmpeg + GPU resources. The 2 s grace window from
// the sibling-kill doesn't apply here — the client has explicitly
// signalled "I'm done with these streams" so we tear them down
// regardless of recent activity. If a second client happens to be
// actively streaming the same file, their next segment request will
// recreate a fresh `v3RunningStream` and restart ffmpeg.
// BumpV3LastAccessed refreshes the `lastAccessed` timestamp on the
// running stream at `dir` so it survives the next `v3MaxIdleTime`
// cleanup pass. Called by the client-side keepalive ping while an
// HLS player is mounted but paused — segment fetches naturally bump
// the timestamp during playback, but a long pause would otherwise
// let the transcode get reaped while the user is clearly still
// engaged. No-op if no stream is registered at `dir` (already
// reaped, never started, or wrong identity).
func (sm *StreamManager) BumpV3LastAccessed(dir string) {
	sm.streamsMutex.Lock()
	defer sm.streamsMutex.Unlock()
	if stream, ok := sm.v3RunningStreams[dir]; ok {
		stream.lastAccessed = time.Now()
	}
}

func (sm *StreamManager) StopV3StreamsForFile(fileID models.FileID, exceptDir string) {
	sm.streamsMutex.Lock()
	defer sm.streamsMutex.Unlock()

	for dir, stream := range sm.v3RunningStreams {
		if stream.vf == nil || stream.vf.ID != fileID {
			continue
		}
		if exceptDir != "" && dir == exceptDir {
			continue
		}
		TranscodeDebugf(
			"[transcode] explicit stop for stream %s (file ID %d)",
			dir, fileID,
		)
		// Drain any waiting segments so their HTTP handlers don't
		// hang waiting for files that will never appear.
		for _, segment := range stream.waitingSegments {
			if len(segment.available) == 0 {
				segment.available <- context.Canceled
			}
		}
		sm.stopV3Transcode(stream)
		sm.removeV3TranscodeFiles(stream)
		delete(sm.v3RunningStreams, dir)
	}
}

// stopAndRemoveAllV3 stops all current streams and removes all cache files
func (sm *StreamManager) stopAndRemoveAllV3() {
	sm.streamsMutex.Lock()
	defer sm.streamsMutex.Unlock()

	for _, stream := range sm.v3RunningStreams {
		for _, segment := range stream.waitingSegments {
			if len(segment.available) == 0 {
				segment.available <- context.Canceled
			}
		}
		sm.stopV3Transcode(stream)
		sm.removeV3TranscodeFiles(stream)
	}

	// ensure nothing else can use the map
	sm.v3RunningStreams = nil
}
