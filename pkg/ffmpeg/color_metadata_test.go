package ffmpeg

import (
	"errors"
	"testing"
)

func TestColorMetadataBitstreamFilter(t *testing.T) {
	tests := []struct {
		codec string
		want  string
		ok    bool
	}{
		{
			codec: "h264",
			want:  "h264_metadata=colour_primaries=1:transfer_characteristics=1:matrix_coefficients=1",
			ok:    true,
		},
		{
			codec: "HVC1",
			want:  "hevc_metadata=colour_primaries=1:transfer_characteristics=1:matrix_coefficients=1",
			ok:    true,
		},
		{
			codec: "vp9",
			ok:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.codec, func(t *testing.T) {
			got, ok := ColorMetadataBitstreamFilter(tt.codec)
			if ok != tt.ok {
				t.Fatalf("ColorMetadataBitstreamFilter(%q) ok = %v, want %v", tt.codec, ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("ColorMetadataBitstreamFilter(%q) = %q, want %q", tt.codec, got, tt.want)
			}
		})
	}
}

func TestIsInvalidColorSpaceError(t *testing.T) {
	if !IsInvalidColorSpaceError(errors.New("ffmpeg failed: Invalid color space")) {
		t.Fatal("expected invalid color space error to match")
	}

	if IsInvalidColorSpaceError(errors.New("ffmpeg failed: unexpected EOF")) {
		t.Fatal("expected unrelated error not to match")
	}
}
