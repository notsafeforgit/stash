package ffmpeg

// CodecInit returns the per-codec encoder argument set used by the
// segmented HLS pipeline. Sets the video codec and codec-specific
// quality/preset/rate-control knobs but nothing that depends on the
// source file or the container — those layer on top in the caller.
//
// Hardware-encoder branches mirror the matching CPU encoder's tuning
// targets (CRF/CQ around 20–25) so quality is roughly comparable across
// codepaths. Real-time presets ("veryfast", "realtime") because this
// runs live during playback.
func CodecInit(codec VideoCodec) (args Args) {
	args = args.VideoCodec(codec)

	switch codec {
	// CPU Codecs
	case VideoCodecLibX264:
		args = append(args,
			"-pix_fmt", "yuv420p",
			"-preset", "veryfast",
			"-crf", "25",
			"-sc_threshold", "0",
			// Profile + level pinned so the master playlist's advertised
			// CODECS string matches what's in the init segment. MSE
			// refuses to decode segments whose actual codec disagrees
			// with the SourceBuffer's declared codec, and SPF reads the
			// SourceBuffer's codec from the master playlist. High @ 5.1
			// covers 4K @ 60fps — high enough that the encoder picks
			// lower actual levels for typical content (the ceiling, not
			// the floor) while still letting 4K@60 transcodes succeed.
			"-profile:v", "high",
			"-level", "5.1",
		)
	case VideoCodecVP9:
		args = append(args,
			"-pix_fmt", "yuv420p",
			"-deadline", "realtime",
			"-cpu-used", "5",
			"-row-mt", "1",
			"-crf", "30",
			"-b:v", "0",
		)
	// HW Codecs
	case VideoCodecN264:
		args = append(args,
			"-rc", "vbr",
			"-cq", "15",
		)
	case VideoCodecN264H:
		args = append(args,
			"-profile", "p7",
			"-tune", "hq",
			"-profile", "high",
			"-rc", "vbr",
			"-rc-lookahead", "60",
			"-surfaces", "64",
			"-spatial-aq", "1",
			"-aq-strength", "15",
			"-cq", "15",
			"-coder", "cabac",
			"-b_ref_mode", "middle",
		)
	case VideoCodecI264, VideoCodecIVP9:
		args = append(args,
			"-global_quality", "20",
			"-preset", "faster",
			// Force every I-frame to be an IDR. h264_qsv otherwise emits
			// non-IDR I-frames at the GOP boundary requested by `-g`, so
			// the HLS muxer can't split a fragment there and falls back
			// to the encoder's natural ~10 s IDR cadence — yielding 10 s
			// segments instead of the 2 s segments the playlist
			// advertises. Pairs with `-g`/`-keyint_min` set in
			// hlsSegmentArgs.
			"-forced_idr", "1",
		)
	case VideoCodecI264C:
		args = append(args,
			"-q", "20",
			"-preset", "faster",
			"-forced_idr", "1",
		)
	case VideoCodecV264, VideoCodecVVP9:
		args = append(args,
			"-qp", "20",
		)
	case VideoCodecA264:
		args = append(args,
			"-quality", "speed",
		)
	case VideoCodecM264:
		args = append(args,
			"-realtime", "1",
		)
	case VideoCodecO264:
		args = append(args,
			"-preset", "superfast",
			"-crf", "25",
		)
	}

	return args
}
