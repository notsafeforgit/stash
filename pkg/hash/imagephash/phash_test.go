package imagephash

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"golang.org/x/image/bmp"
)

type fakeFFmpegOutputGenerator struct {
	data  []byte
	calls int
}

func (f *fakeFFmpegOutputGenerator) GenerateOutput(_ context.Context, _ []string, _ io.Reader) ([]byte, error) {
	f.calls++
	return f.data, nil
}

func TestLoadImageFallsBackToFFmpegOnDecodeError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.jpg")
	if err := os.WriteFile(path, malformedJPEG(t), 0644); err != nil {
		t.Fatal(err)
	}

	var fallback bytes.Buffer
	fallbackImage := image.NewRGBA(image.Rect(0, 0, 1, 1))
	fallbackImage.Set(0, 0, color.White)
	if err := bmp.Encode(&fallback, fallbackImage); err != nil {
		t.Fatal(err)
	}

	encoder := &fakeFFmpegOutputGenerator{
		data: fallback.Bytes(),
	}
	imageFile := &models.ImageFile{
		BaseFile: &models.BaseFile{
			Path: path,
		},
	}

	img, err := loadImage(encoder, imageFile)
	if err != nil {
		t.Fatal(err)
	}

	if encoder.calls != 1 {
		t.Fatalf("ffmpeg fallback calls = %d, want 1", encoder.calls)
	}

	if got := img.Bounds(); got.Dx() != 1 || got.Dy() != 1 {
		t.Fatalf("fallback image bounds = %v, want 1x1", got)
	}
}

func malformedJPEG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: 128, A: 255})
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	for cut := len(data) - 2; cut > 2; cut-- {
		truncated := data[:cut]
		_, _, err := image.Decode(bytes.NewReader(truncated))
		if err != nil && !errors.Is(err, image.ErrFormat) {
			return truncated
		}
	}

	t.Fatal("could not create malformed JPEG that fails after format detection")
	return nil
}
