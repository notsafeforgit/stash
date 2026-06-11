package imagephash

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"

	"github.com/corona10/goimagehash"
	"golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/ffmpeg/transcoder"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
)

type ffmpegOutputGenerator interface {
	GenerateOutput(ctx context.Context, args []string, stdin io.Reader) ([]byte, error)
}

// Generate computes a perceptual hash for an image file.
func Generate(encoder *ffmpeg.FFMpeg, imageFile *models.ImageFile) (*uint64, error) {
	img, err := loadImage(encoder, imageFile)
	if err != nil {
		return nil, fmt.Errorf("loading image: %w", err)
	}

	hash, err := goimagehash.PerceptionHash(img)
	if err != nil {
		return nil, fmt.Errorf("computing phash from image: %w", err)
	}

	hashValue := hash.GetHash()
	return &hashValue, nil
}

// loadImage loads an image from disk and decodes it.
// If Go cannot decode the image, ffmpeg is used to convert to BMP first.
func loadImage(encoder ffmpegOutputGenerator, imageFile *models.ImageFile) (image.Image, error) {
	// try to load with Go's built-in decoders first for better performance
	reader, err := imageFile.Open(&file.OsFS{})
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	img, _, err := image.Decode(reader)
	if err != nil {
		// ffmpeg cannot read files inside zips
		if imageFile.Base().ZipFileID != nil {
			if errors.Is(err, image.ErrFormat) {
				return nil, fmt.Errorf("ffmpeg fallback unsupported for images in zip files")
			}
			return nil, fmt.Errorf("decoding image: %w", err)
		}

		fallbackImg, fallbackErr := loadImageFFmpeg(encoder, imageFile.Path)
		if fallbackErr == nil {
			return fallbackImg, nil
		}

		return nil, fmt.Errorf("decoding image with Go and ffmpeg fallback: %w", errors.Join(err, fallbackErr))
	}

	return img, nil
}

// loadImageFFmpeg uses ffmpeg to convert an image to BMP and then decodes it.
func loadImageFFmpeg(encoder ffmpegOutputGenerator, path string) (image.Image, error) {
	options := transcoder.ScreenshotOptions{
		OutputPath: "-",
		OutputType: transcoder.ScreenshotOutputTypeBMP,
	}

	args := transcoder.ScreenshotTime(path, 0, options)
	img, err := loadImageFFmpegWithDecoder(encoder, args, func(data []byte) (image.Image, error) {
		return bmp.Decode(bytes.NewReader(data))
	})
	if err == nil {
		return img, nil
	}

	options.OutputType = transcoder.ScreenshotOutputTypePNG
	fallbackArgs := transcoder.ScreenshotTime(path, 0, options)
	fallbackImg, fallbackErr := loadImageFFmpegWithDecoder(encoder, fallbackArgs, func(data []byte) (image.Image, error) {
		return png.Decode(bytes.NewReader(data))
	})
	if fallbackErr == nil {
		return fallbackImg, nil
	}

	return nil, fmt.Errorf("decoding ffmpeg image output with png fallback: %w", errors.Join(err, fallbackErr))
}

func loadImageFFmpegWithDecoder(encoder ffmpegOutputGenerator, args ffmpeg.Args, decode func([]byte) (image.Image, error)) (image.Image, error) {
	data, err := encoder.GenerateOutput(context.Background(), args, nil)
	if err != nil {
		return nil, fmt.Errorf("converting image with ffmpeg: %w", err)
	}

	img, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("decoding ffmpeg image output: %w", err)
	}

	return img, nil
}
