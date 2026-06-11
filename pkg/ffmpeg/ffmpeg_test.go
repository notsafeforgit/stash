// Package ffmpeg provides a wrapper around the ffmpeg and ffprobe executables.
package ffmpeg

import "testing"

func TestFFMpegVersion_GreaterThan(t *testing.T) {
	tests := []struct {
		name  string
		this  Version
		other Version
		want  bool
	}{
		{
			"major greater, minor equal, patch equal",
			Version{2, 0, 0},
			Version{1, 0, 0},
			true,
		},
		{
			"major greater, minor less, patch less",
			Version{2, 1, 1},
			Version{1, 0, 0},
			true,
		},
		{
			"major equal, minor greater, patch equal",
			Version{1, 1, 0},
			Version{1, 0, 0},
			true,
		},
		{
			"major equal, minor equal, patch greater",
			Version{1, 0, 1},
			Version{1, 0, 0},
			true,
		},
		{
			"major equal, minor equal, patch equal",
			Version{1, 0, 0},
			Version{1, 0, 0},
			true,
		},
		{
			"major less, minor equal, patch equal",
			Version{1, 0, 0},
			Version{2, 0, 0},
			false,
		},
		{
			"major equal, minor less, patch equal",
			Version{1, 0, 0},
			Version{1, 1, 0},
			false,
		},
		{
			"major equal, minor equal, patch less",
			Version{1, 0, 0},
			Version{1, 0, 1},
			false,
		},
		{
			"major less, minor less, patch less",
			Version{1, 0, 0},
			Version{2, 1, 1},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.this.Gteq(tt.other); got != tt.want {
				t.Errorf("FFMpegVersion.GreaterThan() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBitDepthFromStream(t *testing.T) {
	tests := []struct {
		name string
		in   *FFProbeStream
		want int
	}{
		{
			name: "nil stream",
			want: 0,
		},
		{
			name: "raw sample bits wins",
			in: &FFProbeStream{
				BitsPerRawSample: "10",
				PixFmt:           "yuv420p",
			},
			want: 10,
		},
		{
			name: "invalid raw sample bits falls back to pixel format",
			in: &FFProbeStream{
				BitsPerRawSample: "N/A",
				PixFmt:           "yuv420p",
			},
			want: 8,
		},
		{
			name: "numeric pixel format depth",
			in: &FFProbeStream{
				PixFmt: "yuv420p10le",
			},
			want: 10,
		},
		{
			name: "rgb48 pixel format",
			in: &FFProbeStream{
				PixFmt: "rgb48le",
			},
			want: 16,
		},
		{
			name: "rgba64 pixel format",
			in: &FFProbeStream{
				PixFmt: "rgba64be",
			},
			want: 16,
		},
		{
			name: "common eight bit pixel format",
			in: &FFProbeStream{
				PixFmt: "yuv420p",
			},
			want: 8,
		},
		{
			name: "unknown pixel format",
			in: &FFProbeStream{
				PixFmt: "vendor_unknown",
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bitDepthFromStream(tt.in); got != tt.want {
				t.Errorf("bitDepthFromStream() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHDRTransferDetection(t *testing.T) {
	tests := []struct {
		transfer string
		want     bool
	}{
		{transfer: "smpte2084", want: true},
		{transfer: " arib-std-b67 ", want: true},
		{transfer: "SMPTE2084", want: true},
		{transfer: "bt709", want: false},
		{transfer: "unknown", want: false},
		{transfer: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.transfer, func(t *testing.T) {
			if got := isHDRTransfer(tt.transfer); got != tt.want {
				t.Errorf("isHDRTransfer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHDRColorFlagsUseSourceTags(t *testing.T) {
	color := videoColorMetadata{
		Range:     "tv",
		Space:     "bt2020nc",
		Transfer:  colorTransferHLG,
		Primaries: "bt2020",
	}

	args := hdrColorFlags(color)
	assertArgValue(t, args, "-color_primaries", "bt2020")
	assertArgValue(t, args, "-color_trc", colorTransferHLG)
	assertArgValue(t, args, "-colorspace", "bt2020nc")
	assertArgValue(t, args, "-color_range", "tv")
}

func TestHEVCDownloadEncoderArgsUsesTransferSpecificHDRArgs(t *testing.T) {
	pqArgs := hevcDownloadEncoderArgs(videoColorMetadata{Transfer: colorTransferPQ})
	assertArgValue(t, pqArgs, "-pix_fmt", "yuv420p10le")
	assertArgValue(t, pqArgs, "-color_trc", colorTransferPQ)
	assertArgValue(t, pqArgs, "-x265-params", "repeat-headers=1:hdr10-opt=1")

	hlgArgs := hevcDownloadEncoderArgs(videoColorMetadata{Transfer: colorTransferHLG})
	assertArgValue(t, hlgArgs, "-pix_fmt", "yuv420p10le")
	assertArgValue(t, hlgArgs, "-color_trc", colorTransferHLG)
	assertArgValue(t, hlgArgs, "-x265-params", "repeat-headers=1")

	sdrArgs := hevcDownloadEncoderArgs(videoColorMetadata{Transfer: "bt709"})
	assertArgValue(t, sdrArgs, "-pix_fmt", "yuv420p")
	assertArgAbsent(t, sdrArgs, "-color_trc")
}

func assertArgValue(t *testing.T, args Args, key string, want string) {
	t.Helper()

	for i := 0; i < len(args)-1; i++ {
		if args[i] == key {
			if args[i+1] != want {
				t.Fatalf("arg %q = %q, want %q", key, args[i+1], want)
			}
			return
		}
	}

	t.Fatalf("arg %q not found in %v", key, args)
}

func assertArgAbsent(t *testing.T, args Args, key string) {
	t.Helper()

	for _, arg := range args {
		if arg == key {
			t.Fatalf("arg %q unexpectedly present in %v", key, args)
		}
	}
}
