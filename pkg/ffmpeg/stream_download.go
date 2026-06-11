package ffmpeg

import (
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
)

// DownloadMode controls how the download endpoint encodes the output.
//
// "auto" (default) preserves source codecs and HDR metadata when it
// can: if the source's video + audio fit a fragmented-MP4 container
// (H.264 / HEVC / AV1 video + AAC / MP3 / Opus audio) and the caller
// hasn't requested a resolution downscale, ffmpeg copies the streams
// untouched into MP4. Bitstream is bit-identical to the source, all
// HDR side data (mastering display volume, content light levels,
// HDR10+ dynamic metadata, Dolby Vision config) survives because no
// re-encode happens. Falls through to "h264" for sources outside that
// codec set or when a smaller resolution was asked for.
//
// "copy" forces codec-copy and returns 400 when the source's codecs
// don't fit MP4 — useful for callers that want a hard guarantee of no
// re-encode.
//
// "copy-aac" copies the source video bitstream untouched and
// re-encodes the audio track to AAC. Targets clients that decode the
// source video in MP4 but reject the source audio there (notably iOS
// Safari rejecting Opus-in-MP4 from ManagedMediaSource — same
// constraint as the streaming `hls-copy-fmp4-aac` variant). Cheaper
// than the full H.264 transcode (no video encode) and preserves
// native AV1/HEVC hardware decode on the client. Returns 400 if the
// source video can't ride in MP4 at all (e.g. VP9 source).
//
// "h264" forces re-encode to H.264 + AAC at the requested resolution.
// Plays on essentially anything made in the past decade, at the cost
// of a re-encode pass and SDR yuv420p output (libx264 doesn't carry
// HDR through).
//
// "hevc" forces re-encode to HEVC + AAC, preserving HDR (HDR10 / HLG)
// from the source when the source is HDR. This is the "max-compat
// HDR" answer: HEVC HDR10 in MP4 is the de facto universal HDR
// distribution format — Apple devices, every smart-TV brand's HDR
// profile, Windows 10+ (with the free HEVC extension), modern
// Android, modern set-top boxes and consoles. Useful for AV1 HDR
// sources (which are tiny but only play on a narrow set of devices)
// where the user wants to broaden playability without losing HDR.
// HEVC HDR sources should prefer "copy" (no re-encode), but "hevc"
// detects this and codec-copies in that case rather than wastefully
// re-encoding.
//
// "av1" forces re-encode to AV1 + AAC, preserving HDR from the
// source when the source is HDR. AV1 is more efficient than HEVC at
// equivalent quality (~30% smaller bitstreams), but encode requires
// recent HW (Intel Arc / Meteor Lake+ for QSV/VAAPI, RTX 40-series+
// for NVENC). The server only advertises this mode when HW AV1 is
// available — there's no CPU fallback, since libsvtav1 at typical
// scene resolutions is too slow to be a sensible default. AV1
// sources prefer "copy" (no re-encode), but "av1" detects this and
// codec-copies in that case.
type DownloadMode string

const (
	DownloadModeAuto    DownloadMode = "auto"
	DownloadModeCopy    DownloadMode = "copy"
	DownloadModeCopyAAC DownloadMode = "copy-aac"
	DownloadModeH264    DownloadMode = "h264"
	DownloadModeHEVC    DownloadMode = "hevc"
	DownloadModeAV1     DownloadMode = "av1"
)

func ParseDownloadMode(s string) DownloadMode {
	switch DownloadMode(s) {
	case DownloadModeAuto, DownloadModeCopy, DownloadModeCopyAAC, DownloadModeH264, DownloadModeHEVC, DownloadModeAV1:
		return DownloadMode(s)
	}
	return DownloadModeAuto
}

type videoColorMetadata struct {
	Range     string
	Space     string
	Transfer  string
	Primaries string
}

const (
	colorTransferPQ  = "smpte2084"
	colorTransferHLG = "arib-std-b67"
)

func normalizeColorTag(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	switch value {
	case "", "unknown", "unspecified", "n/a":
		return ""
	default:
		return value
	}
}

// isHDRTransfer reports whether ffprobe's color_transfer value
// indicates an HDR encoding. PQ (smpte2084) is the transfer used by
// HDR10, HDR10+, and Dolby Vision; HLG (arib-std-b67) is the
// broadcast HDR transfer also used in some streaming. Anything else
// (bt709, bt470bg, smpte170m, ...) is SDR.
func isHDRTransfer(transfer string) bool {
	switch normalizeColorTag(transfer) {
	case colorTransferPQ, colorTransferHLG:
		return true
	}
	return false
}

func (m videoColorMetadata) isHDR() bool {
	return isHDRTransfer(m.Transfer)
}

func (m videoColorMetadata) isPQ() bool {
	return normalizeColorTag(m.Transfer) == colorTransferPQ
}

func colorMetadataFromProbe(probe *VideoFile) videoColorMetadata {
	if probe == nil {
		return videoColorMetadata{}
	}

	return videoColorMetadata{
		Range:     normalizeColorTag(probe.ColorRange),
		Space:     normalizeColorTag(probe.ColorSpace),
		Transfer:  normalizeColorTag(probe.ColorTransfer),
		Primaries: normalizeColorTag(probe.ColorPrimaries),
	}
}

func colorMetadataFromModel(vf *models.VideoFile) videoColorMetadata {
	if vf == nil {
		return videoColorMetadata{}
	}

	ret := videoColorMetadata{}
	if vf.ColorRange != nil {
		ret.Range = normalizeColorTag(*vf.ColorRange)
	}
	if vf.ColorSpace != nil {
		ret.Space = normalizeColorTag(*vf.ColorSpace)
	}
	if vf.ColorTransfer != nil {
		ret.Transfer = normalizeColorTag(*vf.ColorTransfer)
	}
	if vf.ColorPrimaries != nil {
		ret.Primaries = normalizeColorTag(*vf.ColorPrimaries)
	}
	return ret
}

type DownloadOptions struct {
	VideoFile  *models.VideoFile
	Mode       DownloadMode
	Resolution models.StreamingResolutionEnum
	// Filename, sans extension, becomes the suggested name in the
	// Content-Disposition header. The handler always appends ".mp4".
	Filename string
}

// encDownload selects which output encoding the download path
// produces. Distinct from DownloadMode (the user-facing knob) because
// "auto" + a copy-eligible source resolves down to encDownloadCopy,
// and "hevc" + an HEVC source likewise short-circuits to copy.
type encDownload int

const (
	encDownloadCopy encDownload = iota
	encDownloadCopyAAC
	encDownloadH264
	encDownloadHEVC
	encDownloadAV1
)

// canCopyVideoToMP4 — audio-agnostic half of canCopyToMP4. Used by the
// copy-aac path which produces AAC regardless of the source audio
// codec, so the source-audio side of canCopyToMP4 doesn't apply.
func canCopyVideoToMP4(videoCodec string) bool {
	switch videoCodec {
	case H264, H265, Hevc, Av1:
		return true
	}
	return false
}

// canServeSourceDirectly reports whether the source file can be
// streamed to the client byte-for-byte (no ffmpeg in the loop) —
// true when the source is already an MP4-family container whose
// codecs MP4 accepts. The download endpoint uses this to short-
// circuit `encDownloadCopy` to `http.ServeFile`, which gives Range
// support (and therefore range-resume on interrupted downloads) for
// free, plus zero CPU cost on the server.
//
// MP4-family detection: the database's `format` field for MP4 / MOV
// / M4V / 3GP all share the ffprobe demuxer name
// "mov,mp4,m4a,3gp,3g2,mj2", so a single equality check covers the
// whole family. WebM, MKV, AVI, etc. fall through to the ffmpeg
// remux path even when their codecs would technically fit MP4 —
// remuxing is unavoidable for those.
//
// Audio handling: the same audio set that's MP4-compatible from
// `canCopyToMP4` is required (Opus inside MP4 plays in browsers but
// is technically a non-standard pairing; we keep it on the standard
// mp4-fits set rather than special-casing). videoOnly sources also
// pass — they're a strict subset of "no audio incompatibility".
func canServeSourceDirectly(vf *models.VideoFile) bool {
	if vf == nil {
		return false
	}
	if vf.Format != Mp4Ffmpeg {
		return false
	}
	audioCodec := MissingUnsupported
	if vf.AudioCodec != "" {
		audioCodec = ProbeAudioCodec(vf.AudioCodec)
	}
	return canCopyToMP4(vf.VideoCodec, audioCodec)
}

// canCopyToMP4 reports whether the source's codecs can be placed in
// an MP4 container without re-encoding. Mirrors the
// `isFMP4RemuxCandidate` gate in `internal/manager/scene.go` (the
// HLS-remux path) — the codec set that fits fragmented-MP4 HLS is
// the same set that fits a downloadable fragmented MP4.
func canCopyToMP4(videoCodec string, audioCodec ProbeAudioCodec) bool {
	switch videoCodec {
	case H264, H265, Hevc, Av1:
	default:
		return false
	}
	switch audioCodec {
	case Aac, Opus, Mp3, MissingUnsupported:
		return true
	}
	return false
}

// ServeDownload streams a downloadable MP4 of the given scene.
//
// Output is fragmented MP4
// (`-movflags +empty_moov+default_base_moof+frag_keyframe`) so
// encoding can stream straight to the response with no temp file —
// first byte arrives within milliseconds of the request, the user
// gets a download progress bar, and a partial download is still
// playable up to the point it stopped. Modern players (browsers,
// QuickTime on macOS Catalina+, Windows 10+, iOS, Android, VLC, mpv)
// handle fragmented MP4 transparently; the only software that
// requires moov-at-start ("faststart") is pre-2015 stuff for which
// nobody is downloading scenes anyway.
//
// Cancellation: the read-lock context inherits from r.Context(), so
// a client disconnect propagates to the ffmpeg `exec.Cmd` via
// CommandContext and the encoder process exits. The lockCtx.Cancel
// in the deferred path also waits up to 5s for the process to exit.
func (sm *StreamManager) ServeDownload(w http.ResponseWriter, r *http.Request, options DownloadOptions) {
	vf := options.VideoFile
	if vf == nil || vf.Path == "" {
		http.Error(w, "scene has no file", http.StatusNotFound)
		return
	}

	audioCodec := MissingUnsupported
	if vf.AudioCodec != "" {
		audioCodec = ProbeAudioCodec(vf.AudioCodec)
	}
	videoOnly := audioCodec == MissingUnsupported

	// HEVC and AV1 modes probe the source for color tags so the
	// encoder can pick the 10-bit profile and preserve PQ vs HLG
	// instead of treating every HDR source as HDR10/PQ. Existing DB
	// metadata is a fallback for older failures; the live ffprobe read
	// wins when available.
	srcColor := colorMetadataFromModel(vf)
	if options.Mode == DownloadModeHEVC || options.Mode == DownloadModeAV1 {
		if probe, err := sm.ffprobe.NewVideoFile(vf.Path); err == nil && probe.VideoStream != nil {
			srcColor = colorMetadataFromProbe(probe)
		}
	}

	keepResolution := options.Resolution == "" ||
		options.Resolution == models.StreamingResolutionEnumOriginal

	encoding := encDownloadH264 // default
	switch options.Mode {
	case DownloadModeCopy:
		if !canCopyToMP4(vf.VideoCodec, audioCodec) {
			http.Error(w, "source codecs don't fit MP4 (copy mode); request mode=h264, mode=hevc, mode=copy-aac, or mode=auto instead", http.StatusBadRequest)
			return
		}
		encoding = encDownloadCopy
	case DownloadModeCopyAAC:
		// Video-side has to fit MP4; audio side doesn't (we re-encode it).
		// Resolution downscale isn't compatible with codec-copy video, so
		// reject if the caller asked for one — they want full transcode
		// in that case.
		if !canCopyVideoToMP4(vf.VideoCodec) {
			http.Error(w, "source video codec doesn't fit MP4 (copy-aac mode); request mode=h264, mode=hevc, or mode=auto instead", http.StatusBadRequest)
			return
		}
		if !keepResolution {
			http.Error(w, "resolution downscale not supported with copy-aac mode (video is codec-copied); request mode=h264 instead", http.StatusBadRequest)
			return
		}
		encoding = encDownloadCopyAAC
	case DownloadModeH264:
		encoding = encDownloadH264
	case DownloadModeHEVC:
		// Source already HEVC → codec-copy is the optimal answer (no
		// re-encode, HDR/SDR preserved exactly). Only re-encode when
		// the source is some other codec OR a downscale was requested.
		if (vf.VideoCodec == H265 || vf.VideoCodec == Hevc) &&
			canCopyToMP4(vf.VideoCodec, audioCodec) &&
			keepResolution {
			encoding = encDownloadCopy
		} else {
			encoding = encDownloadHEVC
		}
	case DownloadModeAV1:
		// Source already AV1 → codec-copy is the optimal answer.
		if vf.VideoCodec == Av1 &&
			canCopyToMP4(vf.VideoCodec, audioCodec) &&
			keepResolution {
			encoding = encDownloadCopy
		} else {
			encoding = encDownloadAV1
		}
	default: // DownloadModeAuto
		if keepResolution && canCopyToMP4(vf.VideoCodec, audioCodec) {
			encoding = encDownloadCopy
		}
	}

	filename := options.Filename
	if filename == "" {
		filename = strings.TrimSuffix(filepath.Base(vf.Path), filepath.Ext(vf.Path))
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".mp4") {
		filename += ".mp4"
	}

	// Static-file fast path: when the source already fits MP4 cleanly
	// (mode resolved to encDownloadCopy, container is mp4-family,
	// codecs canCopyToMP4) we serve the source bytes directly via
	// `http.ServeFile`. Two wins over piping through ffmpeg:
	//   1. Range support — `ServeFile` honours `Range`, `If-Range`,
	//      `If-Modified-Since`, `If-None-Match`. The offline download
	//      worker uses this to resume an interrupted download by
	//      requesting `Range: bytes=N-` against the existing partial,
	//      rather than starting over from byte 0.
	//   2. Bit-identical bytes across attempts — ffmpeg's fragmented-
	//      MP4 muxer (`+empty_moov+default_base_moof+frag_keyframe`)
	//      isn't byte-deterministic across runs, so a partial
	//      ffmpeg-encoded download can't be safely appended to. Static
	//      file bytes are stable.
	// Headers (Content-Type / Content-Disposition) are set before the
	// ServeFile call so the download manager picks them up;
	// `ServeFile` adds Content-Length, Accept-Ranges, Last-Modified,
	// ETag, and handles HEAD itself.
	if encoding == encDownloadCopy && canServeSourceDirectly(vf) {
		w.Header().Set("Content-Type", MimeMp4Video)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
		// Read-lock so a rescan / file move during the download
		// doesn't pull the rug out. Same lifetime as the ffmpeg path.
		lockCtx := sm.lockManager.ReadLock(r.Context(), vf.Path)
		defer lockCtx.Cancel()
		http.ServeFile(w, r, vf.Path)
		return
	}

	w.Header().Set("Content-Type", MimeMp4Video)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	// Deliberately no Content-Length: a streaming transcode pipe
	// doesn't know it. Deliberately no Accept-Ranges: a one-shot
	// ffmpeg pipe can't seek, so advertising byte-ranges would just
	// produce broken resumes from naive download managers.

	if r.Method == http.MethodHead {
		return
	}

	// Read-lock the source so a rescan / file move during the download
	// doesn't pull the rug out. Lock context inherits from r.Context()
	// so client disconnect propagates through to ffmpeg.
	lockCtx := sm.lockManager.ReadLock(r.Context(), vf.Path)
	defer lockCtx.Cancel()

	args := sm.downloadArgs(vf, encoding, options.Resolution, videoOnly, srcColor)
	cmd := sm.encoder.Command(lockCtx, args)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "ffmpeg pipe setup error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	stderr, _ := cmd.StderrPipe()

	logger.Tracef("[download] running %s", cmd)
	if err := cmd.Start(); err != nil {
		http.Error(w, "ffmpeg start error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Drain stderr so ffmpeg doesn't block on it. Capture a bounded
	// tail so we can include it in error logs if Wait reports
	// non-zero exit.
	var stderrTail strings.Builder
	go func() {
		const maxLen = 16 * 1024
		buf := make([]byte, 4*1024)
		for {
			n, rerr := stderr.Read(buf)
			if n > 0 {
				if stderrTail.Len()+n > maxLen {
					s := stderrTail.String()
					stderrTail.Reset()
					stderrTail.WriteString(s[len(s)/2:])
				}
				stderrTail.Write(buf[:n])
			}
			if rerr != nil {
				return
			}
		}
	}()

	if _, err := io.Copy(w, stdout); err != nil {
		// Client disconnect (broken pipe) is the typical case; ffmpeg
		// gets killed by the deferred lockCtx.Cancel.
		logger.Tracef("[download] response copy ended: %v", err)
	}

	if err := cmd.Wait(); err != nil && r.Context().Err() == nil {
		// Only worth logging if the client didn't disconnect — in the
		// disconnect case the non-zero exit is just our own SIGKILL.
		logger.Warnf("[download] ffmpeg exited with error for %s: %v\nstderr tail:\n%s", vf.Path, err, stderrTail.String())
	}
}

// downloadArgs assembles the ffmpeg invocation. Mirrors the
// pre-input → input → post-input ordering in
// `runningStream.makeStreamArgs` so the HW-accel device init lands
// before `-i`.
func (sm *StreamManager) downloadArgs(vf *models.VideoFile, enc encDownload, resolution models.StreamingResolutionEnum, videoOnly bool, srcColor videoColorMetadata) Args {
	extraInputArgs := sm.config.GetLiveTranscodeInputArgs()
	extraOutputArgs := sm.config.GetLiveTranscodeOutputArgs()

	args := Args{"-hide_banner"}
	args = args.LogLevel(LogLevelError)

	var codec VideoCodec
	var maxResolution int
	var fullhw bool

	switch enc {
	case encDownloadH264:
		codec = VideoCodecLibX264
		if hwcodec := sm.encoder.hwCodecHLSCompatible(); hwcodec != nil && sm.config.GetTranscodeHardwareAcceleration() {
			codec = *hwcodec
		}
		maxResolution = resolution.GetMaxResolution()
		fullhw = sm.config.GetTranscodeHardwareAcceleration() && sm.encoder.hwCanFullHWTranscode(sm.context, codec, vf, maxResolution)
		args = sm.encoder.hwDeviceInit(args, codec, fullhw)
	case encDownloadHEVC:
		// HW HEVC if available (VAAPI for now), libx265 otherwise.
		// VAAPI HEVC on modern Intel iGPUs (Tiger Lake+) and AMD VCN
		// (Vega+) preserves HDR10 metadata correctly: input p010 +
		// `-profile:v main10` keeps 10-bit through the pipeline, and
		// `-color_*` flags below write the source color tags into the
		// VUI so the output container's `mdcv`/`clli` boxes carry the
		// HDR side data.
		codec = VideoCodecLibX265
		if hwcodec := sm.encoder.hwCodecHEVCCompatible(); hwcodec != nil && sm.config.GetTranscodeHardwareAcceleration() {
			codec = *hwcodec
		}
		maxResolution = resolution.GetMaxResolution()
		if codec != VideoCodecLibX265 {
			fullhw = sm.config.GetTranscodeHardwareAcceleration() && sm.encoder.hwCanFullHWTranscode(sm.context, codec, vf, maxResolution)
			args = sm.encoder.hwDeviceInit(args, codec, fullhw)
		}
	case encDownloadAV1:
		// HW AV1 only. The server caller (mode=av1) only reaches this
		// branch when ServerCapabilities advertises AV1, which only
		// happens when an HW encoder is present — there's no libsvtav1
		// fallback. If the operator forces mode=av1 with no HW
		// encoder, ffmpeg fails the encode with a clear error from the
		// missing codec; we don't try to silently fall through to CPU
		// because `libsvtav1` real-time encode at typical scene
		// resolutions blows past the user's patience window for an
		// offline-download flow.
		hwcodec := sm.encoder.hwCodecAV1Compatible()
		if hwcodec == nil || !sm.config.GetTranscodeHardwareAcceleration() {
			// No HW AV1 — nothing valid to encode with. Caller should
			// have gated on ServerCapabilities.downloadFormats; if
			// they didn't, ffmpeg's missing-codec error will surface.
			codec = VideoCodec{}
		} else {
			codec = *hwcodec
		}
		maxResolution = resolution.GetMaxResolution()
		if codec.CodeName != "" {
			fullhw = sm.config.GetTranscodeHardwareAcceleration() && sm.encoder.hwCanFullHWTranscode(sm.context, codec, vf, maxResolution)
			args = sm.encoder.hwDeviceInit(args, codec, fullhw)
		}
	}

	args = append(args, extraInputArgs...)
	args = args.Input(vf.Path)

	switch enc {
	case encDownloadCopy:
		args = append(args, "-c:v", "copy")
		if videoOnly {
			args = append(args, "-an")
		} else {
			args = append(args, "-c:a", "copy")
		}
	case encDownloadCopyAAC:
		args = append(args, "-c:v", "copy")
		if videoOnly {
			args = append(args, "-an")
		} else {
			// Stereo AAC at 192k mirrors the H.264 transcode audio
			// settings — the universal "high enough quality, plays
			// everywhere" baseline. asetpts pins the audio track's
			// first-frame PTS to 0 inside the run; same fix as the
			// HLS pipeline for iOS-recorded sources whose container
			// edit lists offset audio relative to video.
			args = append(args,
				"-af", "asetpts=PTS-STARTPTS",
				"-c:a", "aac",
				"-ac", "2",
				"-b:a", "192k",
			)
		}
	case encDownloadH264:
		args = append(args, CodecInit(codec)...)
		videoFilter := sm.encoder.hwMaxResFilter(codec, vf, maxResolution, fullhw)
		args = args.VideoFilter(videoFilter)
		if videoOnly {
			args = append(args, "-an")
		} else {
			args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "192k")
		}
	case encDownloadHEVC:
		if codec == VideoCodecLibX265 {
			args = append(args, hevcDownloadEncoderArgs(srcColor)...)
			if maxResolution > 0 && vf.Height > maxResolution {
				args = args.VideoFilter(VideoFilter("").ScaleHeight(maxResolution))
			}
		} else {
			// HW HEVC (currently VAAPI only). Build the filter chain
			// manually rather than via hwMaxResFilter because the
			// upload format is bit-depth-dependent: HDR sources need
			// p010 surfaces + main10 profile, SDR sources stay on
			// nv12 + main. hwMaxResFilter hardcodes nv12.
			args = append(args, hevcHWEncoderArgs(codec, srcColor)...)
			args = args.VideoFilter(hevcHWVideoFilter(codec, vf, maxResolution, fullhw, srcColor))
		}
		if videoOnly {
			args = append(args, "-an")
		} else {
			args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "192k")
		}
	case encDownloadAV1:
		// HW AV1 (VAAPI / QSV / NVENC). Bit-depth-dependent like HEVC:
		// HDR sources upload p010, SDR upload nv12. Same source color
		// tag block as HEVC writes PQ/HLG correctly into the VUI.
		args = append(args, av1HWEncoderArgs(codec, srcColor)...)
		args = args.VideoFilter(av1HWVideoFilter(codec, vf, maxResolution, fullhw, srcColor))
		if videoOnly {
			args = append(args, "-an")
		} else {
			args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "192k")
		}
	}

	// Fragmented MP4 to stdout. `+empty_moov` writes a moov-with-no-
	// track-durations up front and `+default_base_moof` shaves a few
	// bytes per fragment. `+frag_keyframe` opens a new fragment at
	// each keyframe so the file is playable up to whatever's been
	// downloaded — partial downloads still play up to the cut point.
	args = append(args, "-movflags", "+empty_moov+default_base_moof+frag_keyframe")
	args = append(args, extraOutputArgs...)
	args = append(args, "-f", "mp4", "pipe:1")

	return args
}

// hevcDownloadEncoderArgs returns the libx265 (CPU) HEVC encoder
// argument block.
//
// HDR side data passes through automatically when the bitstream
// carries it: libx265 reads mastering-display (`mdcv`) and
// content-light (`clli`) from input side data and re-emits them on
// output, and HDR10+ dynamic metadata travels in SEI messages
// alongside the bitstream. We add explicit `-color_*` flags so the
// VUI and MP4 container also carry the colour tags. `-tag:v hvc1`
// is required for QuickTime / iOS recognition.
func hevcDownloadEncoderArgs(srcColor videoColorMetadata) Args {
	args := Args{
		"-c:v", "libx265",
		"-preset", "medium",
		"-crf", "23",
		"-tag:v", "hvc1",
	}
	if srcColor.isHDR() {
		args = append(args, "-pix_fmt", "yuv420p10le")
		args = append(args, hdrColorFlags(srcColor)...)
		// repeat-headers: write VPS/SPS/PPS at every keyframe so
		// each fragment of the fMP4 is independently decodable.
		// hdr10-opt is PQ/HDR10-specific; don't apply it to HLG.
		x265Params := "repeat-headers=1"
		if srcColor.isPQ() {
			x265Params += ":hdr10-opt=1"
		}
		args = append(args, "-x265-params", x265Params)
	} else {
		args = append(args,
			"-pix_fmt", "yuv420p",
			"-x265-params", "repeat-headers=1",
		)
	}
	return args
}

// hevcHWEncoderArgs returns the HW HEVC encoder argument block.
// Currently only VAAPI is wired in; if/when other vendors are added
// to `hwCodecHEVCCompatible`, branch here.
//
// VAAPI rate control: `-rc_mode CQP -qp 23` for quality-driven
// encoding. `-global_quality 23` would be the alternative
// (ICQ-equivalent on Intel) but CQP is more predictable across
// driver versions. The download is one-shot so we don't need the
// real-time tuning the HLS path uses.
//
// HDR routing on VAAPI: `-profile:v main10` selects the 10-bit HEVC
// profile, the input p010 surfaces (set up via the filter chain)
// feed it 10-bit data, and the explicit `-color_*` flags below
// write the source color tags into the bitstream VUI. The driver
// passes the `mdcv`/`clli` side data through to the output bitstream
// when present, so the MP4 muxer can write the matching container
// boxes.
func hevcHWEncoderArgs(codec VideoCodec, srcColor videoColorMetadata) Args {
	args := codec.Args()
	args = append(args,
		"-rc_mode", "CQP",
		"-qp", "23",
		"-tag:v", "hvc1",
	)
	if srcColor.isHDR() {
		args = append(args, "-profile:v", "main10")
		args = append(args, hdrColorFlags(srcColor)...)
	} else {
		args = append(args, "-profile:v", "main")
	}
	return args
}

// hevcHWVideoFilter builds the HW HEVC filter chain. Picks p010 (10-bit)
// for HDR sources, nv12 (8-bit) for SDR — built inline rather than via
// hwMaxResFilter because hwMaxResFilter hardcodes nv12, which would
// silently truncate HDR to 8-bit.
//
//   - fullhw: source decoded directly to GPU surfaces; scale + format
//     conversion happen in `scale_vaapi`.
//   - non-fullhw: source decoded on CPU; software scale runs on the
//     CPU pixels (libswscale is HDR-colorspace-aware), then `format`
//     converts to the GPU upload layout, then `hwupload` moves the
//     frame to a VAAPI surface for the encoder.
func hevcHWVideoFilter(codec VideoCodec, vf *models.VideoFile, reqHeight int, fullhw bool, srcColor videoColorMetadata) VideoFilter {
	if codec != VideoCodecV265 {
		return ""
	}

	gpuFmt := "nv12"
	if srcColor.isHDR() {
		gpuFmt = "p010"
	}
	scale := reqHeight > 0 && vf.Height > reqHeight

	var f VideoFilter
	if fullhw {
		if scale {
			w := int(math.Round(float64(vf.Width) * float64(reqHeight) / float64(vf.Height)))
			if w%2 != 0 {
				w++
			}
			f = f.Append(fmt.Sprintf("scale_vaapi=w=%d:h=%d:format=%s", w, reqHeight, gpuFmt))
		} else {
			f = f.Append("scale_vaapi=format=" + gpuFmt)
		}
	} else {
		if scale {
			f = f.ScaleHeight(reqHeight)
		}
		f = f.Append("format=" + gpuFmt)
		f = f.Append("hwupload")
	}
	return f
}

// hdrColorFlags writes source ffprobe color tags into both the
// bitstream VUI and the MP4 container. HDR defaults are only used
// when ffprobe omits an individual tag.
func hdrColorFlags(srcColor videoColorMetadata) Args {
	primaries := srcColor.Primaries
	if primaries == "" {
		primaries = "bt2020"
	}

	transfer := srcColor.Transfer
	if transfer == "" {
		transfer = colorTransferPQ
	}

	space := srcColor.Space
	if space == "" {
		space = "bt2020nc"
	}

	args := Args{
		"-color_primaries", primaries,
		"-color_trc", transfer,
		"-colorspace", space,
	}
	if srcColor.Range != "" {
		args = append(args, "-color_range", srcColor.Range)
	}
	return args
}

// av1HWEncoderArgs returns the HW AV1 encoder argument block. Per-
// vendor RC modes:
//   - VAAPI (`av1_vaapi`): CQP. CQP is most predictable across
//     driver versions; matches the HEVC VAAPI path.
//   - QSV (`av1_qsv`): ICQ via -global_quality (recommended in the
//     ffmpeg docs for quality-driven encoding on Intel Arc / MTL+).
//   - NVENC (`av1_nvenc`): VBR with -cq for capped-quality. NVENC's
//     CQP supports AV1 too, but VBR+CQ is the documented "constant
//     quality" pattern in NVIDIA's encoder guide.
//
// Quality target: AV1's quantiser scale is roughly aligned with HEVC
// — CQ 28 here is comparable to the HEVC paths' CRF/QP 23 since AV1
// is ~20-30% more efficient at the same visual quality.
//
// HDR routing: 10-bit `p010` surfaces flow in (set up via the filter
// chain), the encoder's `main` profile carries 10-bit, and the
// explicit `-color_*` block writes the source color tags. AV1's
// HDR metadata travels in OBU metadata + ITU-T T.35 SEI; vendor
// drivers pass this through when source side data is present.
func av1HWEncoderArgs(codec VideoCodec, srcColor videoColorMetadata) Args {
	args := codec.Args()
	switch codec {
	case VideoCodecVAV1:
		args = append(args, "-rc_mode", "CQP", "-qp", "28")
	case VideoCodecIAV1:
		args = append(args, "-global_quality", "28", "-preset", "faster")
	case VideoCodecNAV1:
		args = append(args, "-rc", "vbr", "-cq", "28", "-preset", "p4")
	}
	if srcColor.isHDR() {
		args = append(args, hdrColorFlags(srcColor)...)
	}
	return args
}

// av1HWVideoFilter builds the HW AV1 filter chain. Bit-depth-dependent
// like the HEVC counterpart (HDR sources upload p010 to keep 10-bit
// through the encoder; SDR sources stay on nv12). Built inline rather
// than via hwMaxResFilter because hwMaxResFilter hardcodes nv12 and
// would silently truncate HDR to 8-bit.
//
// Per-vendor scaler:
//   - VAAPI: scale_vaapi (HW)
//   - QSV: scale_qsv (HW)
//   - NVENC: scale_cuda (HW); software-scale path for non-fullhw uses
//     the standard scale + format + hwupload_cuda chain.
func av1HWVideoFilter(codec VideoCodec, vf *models.VideoFile, reqHeight int, fullhw bool, srcColor videoColorMetadata) VideoFilter {
	gpuFmt := "nv12"
	if srcColor.isHDR() {
		gpuFmt = "p010"
	}
	scale := reqHeight > 0 && vf.Height > reqHeight

	scaledW := func() int {
		w := int(math.Round(float64(vf.Width) * float64(reqHeight) / float64(vf.Height)))
		if w%2 != 0 {
			w++
		}
		return w
	}

	var f VideoFilter
	switch codec {
	case VideoCodecVAV1:
		if fullhw {
			if scale {
				f = f.Append(fmt.Sprintf("scale_vaapi=w=%d:h=%d:format=%s", scaledW(), reqHeight, gpuFmt))
			} else {
				f = f.Append("scale_vaapi=format=" + gpuFmt)
			}
		} else {
			if scale {
				f = f.ScaleHeight(reqHeight)
			}
			f = f.Append("format=" + gpuFmt)
			f = f.Append("hwupload")
		}
	case VideoCodecIAV1:
		if fullhw {
			if scale {
				f = f.Append(fmt.Sprintf("scale_qsv=w=%d:h=%d:format=%s", scaledW(), reqHeight, gpuFmt))
			} else {
				f = f.Append("scale_qsv=format=" + gpuFmt)
			}
		} else {
			if scale {
				f = f.ScaleHeight(reqHeight)
			}
			f = f.Append("hwupload=extra_hw_frames=64")
			f = f.Append("format=qsv")
		}
	case VideoCodecNAV1:
		if fullhw {
			if scale {
				f = f.Append(fmt.Sprintf("scale_cuda=w=%d:h=%d:format=%s", scaledW(), reqHeight, gpuFmt))
			} else {
				f = f.Append("scale_cuda=format=" + gpuFmt)
			}
		} else {
			if scale {
				f = f.ScaleHeight(reqHeight)
			}
			f = f.Append("format=" + gpuFmt)
			f = f.Append("hwupload_cuda")
		}
	}
	return f
}
