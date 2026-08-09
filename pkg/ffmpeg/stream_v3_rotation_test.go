package ffmpeg

import (
	"context"
	"fmt"
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

func TestHardwareDisplayRotationFilter(t *testing.T) {
	tests := []struct {
		name     string
		codec    VideoCodec
		rotation int64
		want     VideoFilter
		wantOK   bool
	}{
		{name: "none", codec: VideoCodecN264, rotation: 0, wantOK: true},
		{name: "CUDA clockwise", codec: VideoCodecN264, rotation: -90, want: "transpose_cuda=dir=clock", wantOK: true},
		{name: "CUDA counterclockwise", codec: VideoCodecN264H, rotation: 90, want: "transpose_cuda=dir=cclock", wantOK: true},
		{name: "QSV clockwise", codec: VideoCodecI264, rotation: -90, want: "vpp_qsv=transpose=clock", wantOK: true},
		{name: "QSV half turn", codec: VideoCodecI264C, rotation: 180, want: "vpp_qsv=transpose=reversal", wantOK: true},
		{name: "VAAPI normalized clockwise", codec: VideoCodecV264, rotation: 270, want: "transpose_vaapi=dir=clock", wantOK: true},
		{name: "software codec", codec: VideoCodecLibX264, rotation: -90},
		{name: "arbitrary CUDA angle", codec: VideoCodecN264, rotation: 45},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotOK := hardwareDisplayRotationFilter(test.codec, test.rotation)
			if got != test.want || gotOK != test.wantOK {
				t.Fatalf("hardwareDisplayRotationFilter(%s, %d) = (%q, %t), want (%q, %t)", test.codec, test.rotation, got, gotOK, test.want, test.wantOK)
			}
		})
	}
}

func TestDisplayRotationInputArgs(t *testing.T) {
	hardwareArgs := displayRotationInputArgs(nil, -90, true)
	if got, want := fmt.Sprint(hardwareArgs), "[-display_rotation:v:0 0]"; got != want {
		t.Fatalf("hardware rotation args = %s, want %s", got, want)
	}

	softwareArgs := displayRotationInputArgs(nil, -90, false)
	if got, want := fmt.Sprint(softwareArgs), "[-autorotate]"; got != want {
		t.Fatalf("software rotation args = %s, want %s", got, want)
	}

	if args := displayRotationInputArgs(nil, 360, true); len(args) != 0 {
		t.Fatalf("full-turn rotation args = %v, want none", args)
	}
}

func TestHardwareRotationRunsBeforeHardwareScaling(t *testing.T) {
	rotationFilter, ok := hardwareDisplayRotationFilter(VideoCodecN264, -90)
	if !ok {
		t.Fatal("CUDA rotation unexpectedly unsupported")
	}
	got := prependVideoFilter(rotationFilter, "scale_cuda=720:1280:format=yuv420p")
	want := VideoFilter("transpose_cuda=dir=clock,scale_cuda=720:1280:format=yuv420p")
	if got != want {
		t.Fatalf("hardware filter chain = %q, want %q", got, want)
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
