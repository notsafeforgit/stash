package videophash

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"

	"github.com/corona10/goimagehash"
	"github.com/disintegration/imaging"
	"golang.org/x/image/bmp"

	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/ffmpeg/transcoder"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
)

const (
	screenshotSize              = 160
	columns                     = 5
	rows                        = 5
	timestampBackoffMaxAttempts = 5
)

type screenshotDecodeError struct {
	format string
	err    error
}

type GenerateResult struct {
	Hash             uint64
	DurationMismatch bool
}

func (e *screenshotDecodeError) Error() string {
	return fmt.Sprintf("decoding %s image: %v", e.format, e.err)
}

func (e *screenshotDecodeError) Unwrap() error {
	return e.err
}

func Generate(encoder *ffmpeg.FFMpeg, videoFile *models.VideoFile) (*uint64, error) {
	result, err := GenerateWithMetadata(encoder, videoFile)
	if err != nil {
		return nil, err
	}

	return &result.Hash, nil
}

func GenerateWithMetadata(encoder *ffmpeg.FFMpeg, videoFile *models.VideoFile) (*GenerateResult, error) {
	sprite, metadata, err := generateSprite(encoder, videoFile)
	if err != nil {
		return nil, err
	}

	hash, err := goimagehash.PerceptionHash(sprite)
	if err != nil {
		return nil, fmt.Errorf("computing phash from sprite: %w", err)
	}
	hashValue := hash.GetHash()
	return &GenerateResult{
		Hash:             hashValue,
		DurationMismatch: metadata.durationMismatch,
	}, nil
}

func generateSpriteScreenshot(encoder *ffmpeg.FFMpeg, input string, t float64, slowSeek bool, setBT709ColorParameters bool) (image.Image, error) {
	options := transcoder.ScreenshotOptions{
		Width:                   screenshotSize,
		OutputPath:              "-",
		OutputType:              transcoder.ScreenshotOutputTypeBMP,
		SlowSeek:                slowSeek,
		SetBT709ColorParameters: setBT709ColorParameters,
	}

	bmpArgs := transcoder.ScreenshotTime(input, t, options)
	options.OutputType = transcoder.ScreenshotOutputTypePNG
	pngArgs := transcoder.ScreenshotTime(input, t, options)

	return generateScreenshotWithFallback(encoder, bmpArgs, pngArgs)
}

func generateSpriteFrameScreenshot(encoder *ffmpeg.FFMpeg, input string, frame int, setBT709ColorParameters bool) (image.Image, error) {
	options := transcoder.ScreenshotOptions{
		Width:                   screenshotSize,
		OutputPath:              "-",
		OutputType:              transcoder.ScreenshotOutputTypeBMP,
		SetBT709ColorParameters: setBT709ColorParameters,
	}

	bmpArgs := transcoder.ScreenshotFrame(input, frame, options)
	options.OutputType = transcoder.ScreenshotOutputTypePNG
	pngArgs := transcoder.ScreenshotFrame(input, frame, options)

	return generateScreenshotWithFallback(encoder, bmpArgs, pngArgs)
}

func generateScreenshotWithFallback(encoder *ffmpeg.FFMpeg, bmpArgs ffmpeg.Args, pngArgs ffmpeg.Args) (image.Image, error) {
	img, err := generateScreenshot(encoder, bmpArgs, "bmp", func(data []byte) (image.Image, error) {
		return bmp.Decode(bytes.NewReader(data))
	})
	if err == nil {
		return img, nil
	}

	var decodeErr *screenshotDecodeError
	if !errors.As(err, &decodeErr) {
		return nil, err
	}

	img, fallbackErr := generateScreenshot(encoder, pngArgs, "png", func(data []byte) (image.Image, error) {
		return png.Decode(bytes.NewReader(data))
	})
	if fallbackErr == nil {
		return img, nil
	}

	return nil, fmt.Errorf("decoding screenshot output with png fallback: %w", errors.Join(err, fallbackErr))
}

func generateScreenshot(encoder *ffmpeg.FFMpeg, args ffmpeg.Args, format string, decode func([]byte) (image.Image, error)) (image.Image, error) {
	data, err := encoder.GenerateOutput(context.Background(), args, nil)
	if err != nil {
		return nil, err
	}

	img, err := decode(data)
	if err != nil {
		return nil, &screenshotDecodeError{
			format: format,
			err:    err,
		}
	}

	return img, nil
}

func combineImages(images []image.Image) image.Image {
	width := images[0].Bounds().Size().X
	height := images[0].Bounds().Size().Y
	canvasWidth := width * columns
	canvasHeight := height * rows
	montage := imaging.New(canvasWidth, canvasHeight, color.NRGBA{})
	for index := 0; index < len(images); index++ {
		x := width * (index % columns)
		y := height * int(math.Floor(float64(index)/float64(rows)))
		img := images[index]
		montage = imaging.Paste(montage, img, image.Pt(x, y))
	}

	return montage
}

type spriteMetadata struct {
	durationMismatch bool
}

func generateSprite(encoder *ffmpeg.FFMpeg, videoFile *models.VideoFile) (image.Image, spriteMetadata, error) {
	logger.Infof("[generator] generating phash sprite for %s", videoFile.Path)

	metadata := spriteMetadata{}
	duration := videoFile.VideoStreamDurationFinite()
	frameCount := estimateFrameCount(videoFile)
	if frameCount > 0 && frameCount <= columns*rows {
		img, err := generateFrameSprite(encoder, videoFile, frameCount)
		return img, metadata, err
	}

	// Generate sprite image offset by 5% on each end to avoid intro/outros
	chunkCount := columns * rows
	offset := 0.05 * duration
	stepSize := (0.9 * duration) / float64(chunkCount)
	var images []image.Image
	slowSeek := false
	setBT709ColorParameters := false

	for i := 0; i < chunkCount; i++ {
		time := offset + (float64(i) * stepSize)

		img, err := generateSpriteScreenshot(encoder, videoFile.Path, time, slowSeek, setBT709ColorParameters)
		if err != nil && !slowSeek {
			logger.Warnf("[generator] fast phash screenshot seek failed for %s at %.3fs, retrying with accurate seek for remaining phash screenshots: %v", videoFile.Path, time, err)

			slowSeek = true
			img, err = generateSpriteScreenshot(encoder, videoFile.Path, time, slowSeek, setBT709ColorParameters)
		}
		if err != nil && !setBT709ColorParameters {
			logger.Warnf("[generator] phash screenshot failed for %s at %.3fs, retrying with BT.709 color metadata fallback for remaining phash screenshots: %v", videoFile.Path, time, err)

			setBT709ColorParameters = true
			img, err = generateSpriteScreenshot(encoder, videoFile.Path, time, slowSeek, setBT709ColorParameters)
		}
		if err != nil {
			var usedTimestampBackoff bool
			img, usedTimestampBackoff, err = generateSpriteScreenshotWithTimestampBackoff(encoder, videoFile.Path, time, stepSize, slowSeek, setBT709ColorParameters, err)
			metadata.durationMismatch = metadata.durationMismatch || usedTimestampBackoff
		}
		if err != nil {
			return nil, metadata, fmt.Errorf("generating sprite screenshot: %w", err)
		}

		images = append(images, img)
	}

	// Combine all of the thumbnails into a sprite image
	if len(images) == 0 {
		return nil, metadata, fmt.Errorf("images slice is empty, failed to generate phash sprite for %s", videoFile.Path)
	}

	return combineImages(images), metadata, nil
}

func generateSpriteScreenshotWithTimestampBackoff(encoder *ffmpeg.FFMpeg, input string, t float64, stepSize float64, slowSeek bool, setBT709ColorParameters bool, originalErr error) (image.Image, bool, error) {
	lastErr := originalErr
	for _, fallbackTime := range timestampBackoffTimes(t, stepSize) {
		logger.Warnf("[generator] phash screenshot failed for %s at %.3fs, retrying earlier timestamp %.3fs: %v", input, t, fallbackTime, lastErr)

		img, err := generateSpriteScreenshot(encoder, input, fallbackTime, slowSeek, setBT709ColorParameters)
		if err == nil {
			return img, true, nil
		}
		lastErr = err
	}

	return nil, false, originalErr
}

func timestampBackoffTimes(t float64, stepSize float64) []float64 {
	if stepSize <= 0 || t <= 0 {
		return nil
	}

	ret := make([]float64, 0, timestampBackoffMaxAttempts)
	for attempt := 1; attempt <= timestampBackoffMaxAttempts; attempt++ {
		fallbackTime := t - (float64(attempt) * stepSize)
		if fallbackTime < 0 {
			break
		}

		ret = append(ret, fallbackTime)
	}

	return ret
}

func generateFrameSprite(encoder *ffmpeg.FFMpeg, videoFile *models.VideoFile, frameCount int) (image.Image, error) {
	chunkCount := columns * rows
	images := make([]image.Image, 0, chunkCount)
	setBT709ColorParameters := false

	for i := 0; i < chunkCount; i++ {
		frame := spriteFrameIndex(i, frameCount)

		img, err := generateSpriteFrameScreenshot(encoder, videoFile.Path, frame, setBT709ColorParameters)
		if err != nil && !setBT709ColorParameters {
			logger.Warnf("[generator] frame-based phash screenshot failed for %s at frame %d, retrying with BT.709 color metadata fallback for remaining phash screenshots: %v", videoFile.Path, frame, err)

			setBT709ColorParameters = true
			img, err = generateSpriteFrameScreenshot(encoder, videoFile.Path, frame, setBT709ColorParameters)
		}
		if err != nil {
			return nil, fmt.Errorf("generating frame-based sprite screenshot: %w", err)
		}

		images = append(images, img)
	}

	return combineImages(images), nil
}

func estimateFrameCount(videoFile *models.VideoFile) int {
	if videoFile.FrameCount != nil && *videoFile.FrameCount > 0 && *videoFile.FrameCount <= int64(maxInt()) {
		return int(*videoFile.FrameCount)
	}

	duration := videoFile.VideoStreamDurationFinite()
	frameRate := videoFile.FrameRateFinite()
	if duration <= 0 || frameRate <= 0 {
		return 0
	}

	frameCount := int(math.Round(duration * frameRate))
	if frameCount < 1 {
		return 1
	}

	return frameCount
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

func spriteFrameIndex(spriteIndex int, frameCount int) int {
	if frameCount <= 1 {
		return 0
	}

	return int(math.Round(float64(spriteIndex) * float64(frameCount-1) / float64(columns*rows-1)))
}
