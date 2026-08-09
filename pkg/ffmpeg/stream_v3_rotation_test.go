package ffmpeg

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
)

type rotationTestStreamConfig struct{}

func (rotationTestStreamConfig) GetMaxStreamingTranscodeSize() models.StreamingResolutionEnum {
	return models.StreamingResolutionEnumOriginal
}

func (rotationTestStreamConfig) GetLiveTranscodeInputArgs() []string  { return nil }
func (rotationTestStreamConfig) GetLiveTranscodeOutputArgs() []string { return nil }
func (rotationTestStreamConfig) GetTranscodeHardwareAcceleration() bool {
	return false
}

func TestCanUseFullHardwareDecode(t *testing.T) {
	tests := []struct {
		name     string
		rotation int64
		want     bool
	}{
		{name: "none", rotation: 0, want: true},
		{name: "clockwise", rotation: -90, want: false},
		{name: "counterclockwise", rotation: 90, want: false},
		{name: "half turn", rotation: 180, want: false},
		{name: "normalized full turn", rotation: 360, want: true},
		{name: "normalized clockwise", rotation: 270, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canUseFullHardwareDecode(test.rotation); got != test.want {
				t.Fatalf("canUseFullHardwareDecode(%d) = %t, want %t", test.rotation, got, test.want)
			}
		})
	}
}

func TestRotatedHLSArgsEnableAutorotateBeforeInput(t *testing.T) {
	stream := &v3RunningStream{
		streamType: V3StreamTypeHLS,
		vf: &models.VideoFile{
			BaseFile:   &models.BaseFile{Path: "rotated.mov"},
			Width:      1080,
			Height:     1920,
			FrameRate:  30,
			AudioCodec: "aac",
		},
		outputDir: t.TempDir(),
	}
	stream.displayRotationOnce.Do(func() {
		stream.displayRotation = -90
	})

	sm := &StreamManager{
		encoder: &FFMpeg{},
		config:  rotationTestStreamConfig{},
		context: context.Background(),
	}
	args := stream.makeStreamArgs(sm, 0)

	autorotateIndex := -1
	inputIndex := -1
	for i, arg := range args {
		switch arg {
		case "-autorotate":
			autorotateIndex = i
		case "-i":
			inputIndex = i
		}
	}
	if autorotateIndex == -1 {
		t.Fatal("rotated HLS args do not enable autorotation")
	}
	if inputIndex == -1 || autorotateIndex > inputIndex {
		t.Fatalf("autorotate must be an input option: args=%v", args)
	}
}
