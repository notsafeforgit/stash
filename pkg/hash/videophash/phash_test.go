package videophash

import (
	"errors"
	"math"
	"testing"

	"github.com/stashapp/stash/pkg/models"
)

func TestEstimateFrameCount(t *testing.T) {
	tests := []struct {
		name      string
		videoFile *models.VideoFile
		want      int
	}{
		{
			name: "short social clip",
			videoFile: &models.VideoFile{
				Duration:  0.54,
				FrameRate: 100.0 / 9.0,
			},
			want: 6,
		},
		{
			name: "missing frame rate",
			videoFile: &models.VideoFile{
				Duration: 0.54,
			},
			want: 0,
		},
		{
			name: "stored frame count",
			videoFile: &models.VideoFile{
				Duration:   10,
				FrameRate:  30,
				FrameCount: int64Ptr(6),
			},
			want: 6,
		},
		{
			name: "minimum positive frame count",
			videoFile: &models.VideoFile{
				Duration:  0.01,
				FrameRate: 1,
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := estimateFrameCount(tt.videoFile); got != tt.want {
				t.Fatalf("estimateFrameCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}

func TestSpriteFrameIndex(t *testing.T) {
	tests := []struct {
		name        string
		spriteIndex int
		frameCount  int
		want        int
	}{
		{
			name:        "first tile",
			spriteIndex: 0,
			frameCount:  6,
			want:        0,
		},
		{
			name:        "middle tile",
			spriteIndex: 12,
			frameCount:  6,
			want:        3,
		},
		{
			name:        "last tile",
			spriteIndex: 24,
			frameCount:  6,
			want:        5,
		},
		{
			name:        "single frame",
			spriteIndex: 24,
			frameCount:  1,
			want:        0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := spriteFrameIndex(tt.spriteIndex, tt.frameCount); got != tt.want {
				t.Fatalf("spriteFrameIndex(%d, %d) = %d, want %d", tt.spriteIndex, tt.frameCount, got, tt.want)
			}
		})
	}
}

func TestTimestampBackoffTimes(t *testing.T) {
	got := timestampBackoffTimes(13.938, 0.549)
	want := []float64{13.389, 12.84, 11.742, 9.546, 5.154}

	if len(got) != len(want) {
		t.Fatalf("timestampBackoffTimes() length = %d, want %d", len(got), len(want))
	}

	for i := range want {
		if math.Abs(got[i]-want[i]) > 0.000001 {
			t.Fatalf("timestampBackoffTimes()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestTimestampBackoffTimesStopsAtZero(t *testing.T) {
	got := timestampBackoffTimes(1.0, 0.4)
	want := []float64{0.6, 0.19999999999999996}

	if len(got) != len(want) {
		t.Fatalf("timestampBackoffTimes() length = %d, want %d", len(got), len(want))
	}

	for i := range want {
		if math.Abs(got[i]-want[i]) > 0.000001 {
			t.Fatalf("timestampBackoffTimes()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestFrameBackoffIndexes(t *testing.T) {
	got := frameBackoffIndexes(6)
	want := []int{5, 4, 2}

	if len(got) != len(want) {
		t.Fatalf("frameBackoffIndexes() length = %d, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("frameBackoffIndexes()[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

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
			got, ok := colorMetadataBitstreamFilter(tt.codec)
			if ok != tt.ok {
				t.Fatalf("colorMetadataBitstreamFilter(%q) ok = %v, want %v", tt.codec, ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("colorMetadataBitstreamFilter(%q) = %q, want %q", tt.codec, got, tt.want)
			}
		})
	}
}

func TestIsInvalidColorSpaceError(t *testing.T) {
	if !isInvalidColorSpaceError(errors.New("ffmpeg failed: Invalid color space")) {
		t.Fatal("expected invalid color space error to match")
	}

	if isInvalidColorSpaceError(errors.New("ffmpeg failed: unexpected EOF")) {
		t.Fatal("expected unrelated error not to match")
	}
}
