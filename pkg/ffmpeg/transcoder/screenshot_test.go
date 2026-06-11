package transcoder

import (
	"reflect"
	"testing"
)

func TestScreenshotTimeDefaultUsesFastSeek(t *testing.T) {
	options := ScreenshotOptions{
		OutputPath: "out.jpg",
		OutputType: ScreenshotOutputTypeImage2,
	}

	got := ScreenshotTime("input.webm", 12.5, options)
	want := []string{
		"-v", "error",
		"-y",
		"-ss", "12.5",
		"-i", "input.webm",
		"-frames:v", "1",
		"-strict", "-2",
		"-f", "image2",
		"out.jpg",
	}

	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("ScreenshotTime() = %#v, want %#v", []string(got), want)
	}
}

func TestScreenshotTimeSlowSeek(t *testing.T) {
	options := ScreenshotOptions{
		OutputPath: "out.jpg",
		OutputType: ScreenshotOutputTypeImage2,
		SlowSeek:   true,
	}

	got := ScreenshotTime("input.webm", 12.5, options)
	want := []string{
		"-v", "error",
		"-y",
		"-i", "input.webm",
		"-ss", "12.5",
		"-frames:v", "1",
		"-strict", "-2",
		"-f", "image2",
		"out.jpg",
	}

	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("ScreenshotTime() = %#v, want %#v", []string(got), want)
	}
}

func TestScreenshotTimeColorParameterFallback(t *testing.T) {
	options := ScreenshotOptions{
		OutputPath:              "out.jpg",
		OutputType:              ScreenshotOutputTypeImage2,
		Width:                   160,
		SetBT709ColorParameters: true,
	}

	got := ScreenshotTime("input.webm", 12.5, options)
	want := []string{
		"-v", "error",
		"-y",
		"-ss", "12.5",
		"-colorspace", "bt709",
		"-color_trc", "bt709",
		"-color_primaries", "bt709",
		"-i", "input.webm",
		"-frames:v", "1",
		"-vf", "setparams=colorspace=bt709:color_primaries=bt709:color_trc=bt709,scale=160:-2",
		"-strict", "-2",
		"-f", "image2",
		"out.jpg",
	}

	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("ScreenshotTime() = %#v, want %#v", []string(got), want)
	}
}

func TestScreenshotTimeBMPPipe(t *testing.T) {
	options := ScreenshotOptions{
		OutputPath: "-",
		OutputType: ScreenshotOutputTypeBMP,
		Width:      160,
	}

	got := ScreenshotTime("input.webm", 12.5, options)
	want := []string{
		"-v", "error",
		"-y",
		"-ss", "12.5",
		"-i", "input.webm",
		"-frames:v", "1",
		"-vf", "scale=160:-2",
		"-c:v", "bmp",
		"-f", "image2pipe",
		"-",
	}

	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("ScreenshotTime() = %#v, want %#v", []string(got), want)
	}
}

func TestScreenshotFrameColorParameterFallback(t *testing.T) {
	options := ScreenshotOptions{
		OutputPath:              "-",
		OutputType:              ScreenshotOutputTypeBMP,
		Width:                   160,
		SetBT709ColorParameters: true,
	}

	got := ScreenshotFrame("input.webm", 12, options)
	want := []string{
		"-v", "error",
		"-y",
		"-colorspace", "bt709",
		"-color_trc", "bt709",
		"-color_primaries", "bt709",
		"-i", "input.webm",
		"-frames:v", "1",
		"-vsync", "0",
		"-vf", "setparams=colorspace=bt709:color_primaries=bt709:color_trc=bt709,select=eq(n\\,12),scale=160:-2",
		"-c:v", "bmp",
		"-f", "image2pipe",
		"-",
	}

	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("ScreenshotFrame() = %#v, want %#v", []string(got), want)
	}
}
