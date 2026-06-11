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
	"strings"

	"github.com/corona10/goimagehash"
	"github.com/disintegration/imaging"
	"golang.org/x/image/bmp"

	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/ffmpeg/transcoder"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
)

const (
	screenshotSize = 160
	columns        = 5
	rows           = 5
)

var timestampBackoffMultipliers = []float64{1, 2, 4, 8, 16, 32}

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
		img, recoveredFrameMismatch, err := generateFrameSprite(encoder, videoFile, frameCount)
		metadata.durationMismatch = metadata.durationMismatch || recoveredFrameMismatch
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
			logger.Warnf("[generator] fast phash screenshot seek failed for %s at %.3fs, retrying with accurate seek for remaining phash screenshots: %s", videoFile.Path, time, compactError(err))

			slowSeek = true
			img, err = generateSpriteScreenshot(encoder, videoFile.Path, time, slowSeek, setBT709ColorParameters)
		}
		if err != nil && !setBT709ColorParameters {
			logger.Warnf("[generator] phash screenshot failed for %s at %.3fs, retrying with BT.709 color metadata fallback for remaining phash screenshots: %s", videoFile.Path, time, compactError(err))

			setBT709ColorParameters = true
			img, err = generateSpriteScreenshot(encoder, videoFile.Path, time, slowSeek, setBT709ColorParameters)
		}
		if err != nil {
			var usedTimestampBackoff bool
			img, usedTimestampBackoff, err = generateSpriteScreenshotWithTimestampBackoff(encoder, videoFile.Path, time, stepSize, slowSeek, setBT709ColorParameters, err)
			metadata.durationMismatch = metadata.durationMismatch || usedTimestampBackoff
		}
		if err != nil && len(images) > 0 {
			logger.Warnf("[generator] phash screenshot failed for %s at %.3fs after timestamp fallbacks, reusing previous sprite frame: %s", videoFile.Path, time, compactError(err))
			metadata.durationMismatch = true
			images = append(images, images[len(images)-1])
			continue
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
	for _, fallbackTime := range timestampBackoffTimes(t, stepSize) {
		img, err := generateSpriteScreenshot(encoder, input, fallbackTime, slowSeek, setBT709ColorParameters)
		if err == nil {
			logger.Warnf("[generator] recovered phash screenshot for %s at %.3fs using earlier timestamp %.3fs", input, t, fallbackTime)
			return img, true, nil
		}
		logger.Debugf("[generator] phash screenshot fallback failed for %s at %.3fs while recovering %.3fs: %s", input, fallbackTime, t, compactError(err))
	}

	return nil, false, originalErr
}

func timestampBackoffTimes(t float64, stepSize float64) []float64 {
	if stepSize <= 0 || t <= 0 {
		return nil
	}

	ret := make([]float64, 0, len(timestampBackoffMultipliers))
	for _, multiplier := range timestampBackoffMultipliers {
		fallbackTime := t - (multiplier * stepSize)
		if fallbackTime < 0 {
			break
		}

		ret = append(ret, fallbackTime)
	}

	return ret
}

func frameBackoffIndexes(frame int) []int {
	if frame <= 0 {
		return nil
	}

	ret := make([]int, 0, len(timestampBackoffMultipliers))
	for _, multiplier := range timestampBackoffMultipliers {
		fallbackFrame := frame - int(multiplier)
		if fallbackFrame < 0 {
			break
		}

		ret = append(ret, fallbackFrame)
	}

	return ret
}

func generateFrameSprite(encoder *ffmpeg.FFMpeg, videoFile *models.VideoFile, frameCount int) (image.Image, bool, error) {
	chunkCount := columns * rows
	images := make([]image.Image, 0, chunkCount)
	setBT709ColorParameters := false
	durationMismatch := false

	for i := 0; i < chunkCount; i++ {
		frame := spriteFrameIndex(i, frameCount)

		img, err := generateSpriteFrameScreenshot(encoder, videoFile.Path, frame, setBT709ColorParameters)
		if err != nil && !setBT709ColorParameters {
			logger.Warnf("[generator] frame-based phash screenshot failed for %s at frame %d, retrying with BT.709 color metadata fallback for remaining phash screenshots: %s", videoFile.Path, frame, compactError(err))

			setBT709ColorParameters = true
			img, err = generateSpriteFrameScreenshot(encoder, videoFile.Path, frame, setBT709ColorParameters)
		}
		if err != nil {
			var usedFrameBackoff bool
			img, usedFrameBackoff, err = generateSpriteFrameScreenshotWithFrameBackoff(encoder, videoFile.Path, frame, setBT709ColorParameters, err)
			durationMismatch = durationMismatch || usedFrameBackoff
		}
		if err != nil && len(images) > 0 {
			logger.Warnf("[generator] frame-based phash screenshot failed for %s at frame %d after frame fallbacks, reusing previous sprite frame: %s", videoFile.Path, frame, compactError(err))
			durationMismatch = true
			images = append(images, images[len(images)-1])
			continue
		}
		if err != nil {
			return nil, durationMismatch, fmt.Errorf("generating frame-based sprite screenshot: %w", err)
		}

		images = append(images, img)
	}

	return combineImages(images), durationMismatch, nil
}

func generateSpriteFrameScreenshotWithFrameBackoff(encoder *ffmpeg.FFMpeg, input string, frame int, setBT709ColorParameters bool, originalErr error) (image.Image, bool, error) {
	for _, fallbackFrame := range frameBackoffIndexes(frame) {
		img, err := generateSpriteFrameScreenshot(encoder, input, fallbackFrame, setBT709ColorParameters)
		if err == nil {
			logger.Warnf("[generator] recovered frame-based phash screenshot for %s at frame %d using earlier frame %d", input, frame, fallbackFrame)
			return img, true, nil
		}
		logger.Debugf("[generator] frame-based phash screenshot fallback failed for %s at frame %d while recovering frame %d: %s", input, fallbackFrame, frame, compactError(err))
	}

	return nil, false, originalErr
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

func compactError(err error) string {
	if err == nil {
		return ""
	}

	return strings.ReplaceAll(err.Error(), "\n", "; ")
}

func spriteFrameIndex(spriteIndex int, frameCount int) int {
	if frameCount <= 1 {
		return 0
	}

	return int(math.Round(float64(spriteIndex) * float64(frameCount-1) / float64(columns*rows-1)))
}
