package ffmpeg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStreamRotation(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		sideData []FFProbeStreamSideData
		want     int64
	}{
		{name: "none", want: 0},
		{name: "legacy clockwise", tag: "90", want: 90},
		{name: "legacy counterclockwise", tag: "-90", want: -90},
		{name: "normalizes positive full turns", tag: "450", want: 90},
		{name: "normalizes negative full turns", tag: "-450", want: -90},
		{name: "normalizes negative half turn", tag: "-180", want: 180},
		{
			name:     "display matrix takes precedence",
			tag:      "90",
			sideData: []FFProbeStreamSideData{{Rotation: -90}},
			want:     -90,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stream := &FFProbeStream{SideDataList: test.sideData}
			stream.Tags.Rotate = test.tag
			if got := streamRotation(stream); got != test.want {
				t.Fatalf("streamRotation() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestRotationSwapsDimensions(t *testing.T) {
	tests := []struct {
		rotation int64
		want     bool
	}{
		{rotation: 0, want: false},
		{rotation: 90, want: true},
		{rotation: -90, want: true},
		{rotation: 180, want: false},
	}

	for _, test := range tests {
		if got := rotationSwapsDimensions(test.rotation); got != test.want {
			t.Errorf("rotationSwapsDimensions(%d) = %t, want %t", test.rotation, got, test.want)
		}
	}
}

func TestParseStoresRotationAndDisplayDimensions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rotated.mov")
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}

	probeJSON := &FFProbeJSON{
		Streams: []FFProbeStream{{
			CodecType:    "video",
			Width:        1920,
			Height:       1080,
			SideDataList: []FFProbeStreamSideData{{Rotation: -90}},
		}},
	}
	video, err := parse(path, probeJSON)
	if err != nil {
		t.Fatal(err)
	}

	if video.Rotation != -90 {
		t.Errorf("Rotation = %d, want -90", video.Rotation)
	}
	if video.Width != 1080 || video.Height != 1920 {
		t.Errorf("display dimensions = %dx%d, want 1080x1920", video.Width, video.Height)
	}
}
