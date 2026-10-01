package video

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
)

// Decorator adds video specific fields to a File.
type Decorator struct {
	FFProbe *ffmpeg.FFProbe
}

var ErrNoVideoStream = errors.New("media has no visual video stream")

func (d *Decorator) Decorate(ctx context.Context, fs models.FS, f models.File) (models.File, error) {
	if d.FFProbe == nil {
		return f, errors.New("ffprobe not configured")
	}

	base := f.Base()
	probe := d.FFProbe
	var videoFile *ffmpeg.VideoFile
	var err error
	if opened, ok := fs.(file.OpenedFileProvider); ok {
		var descriptor *os.File
		descriptor, err = opened.BorrowFile(base.Path)
		if err == nil {
			videoFile, err = probe.NewVideoFileFromOpen(ctx, descriptor, base.Path)
		}
	} else if _, isOs := fs.(*file.OsFS); isOs {
		videoFile, err = probe.NewVideoFile(base.Path)
	} else {
		return f, fmt.Errorf("video.constructFile: filesystem cannot provide a seekable media file")
	}
	if err != nil {
		return f, fmt.Errorf("running ffprobe on %q: %w", base.Path, err)
	}
	if videoFile.VideoStream == nil || videoFile.Width <= 0 || videoFile.Height <= 0 {
		return f, ErrNoVideoStream
	}

	container, err := ffmpeg.MatchContainer(videoFile.Container, base.Path)
	if err != nil {
		return f, fmt.Errorf("matching container for %q: %w", base.Path, err)
	}

	// check if there is a funscript file
	interactive := false
	if _, err := fs.Lstat(GetFunscriptPath(base.Path)); err == nil {
		interactive = true
	}

	return &models.VideoFile{
		BaseFile:            base,
		Format:              string(container),
		VideoCodec:          videoFile.VideoCodec,
		AudioCodec:          videoFile.AudioCodec,
		Width:               videoFile.Width,
		Height:              videoFile.Height,
		Duration:            videoFile.FileDuration,
		VideoStreamDuration: videoStreamDurationPtr(videoFile),
		FrameRate:           videoFile.FrameRate,
		FrameCount:          int64PtrIfPositive(videoFile.FrameCount),
		DurationMismatch:    videoDurationMismatch(videoFile),
		BitRate:             videoFile.Bitrate,
		BitDepth:            intPtrIfPositive(videoFile.BitDepth),
		ColorRange:          stringPtrIfNotEmpty(videoFile.ColorRange),
		ColorSpace:          stringPtrIfNotEmpty(videoFile.ColorSpace),
		ColorTransfer:       stringPtrIfNotEmpty(videoFile.ColorTransfer),
		ColorPrimaries:      stringPtrIfNotEmpty(videoFile.ColorPrimaries),
		Interactive:         interactive,
	}, nil
}

func intPtrIfPositive(v int) *int {
	if v <= 0 {
		return nil
	}

	return &v
}

func int64PtrIfPositive(v int64) *int64 {
	if v <= 0 {
		return nil
	}

	return &v
}

func float64PtrIfPositive(v float64) *float64 {
	if v <= 0 {
		return nil
	}

	return &v
}

func videoStreamDurationPtr(v *ffmpeg.VideoFile) *float64 {
	if !v.HasVideoStreamDuration {
		return nil
	}

	return float64PtrIfPositive(v.VideoStreamDuration)
}

func videoDurationMismatch(v *ffmpeg.VideoFile) bool {
	return v.HasVideoStreamDuration && math.Abs(v.FileDuration-v.VideoStreamDuration) > 0.5
}

func stringPtrIfNotEmpty(v string) *string {
	if v == "" {
		return nil
	}

	return &v
}

func (d *Decorator) IsMissingMetadata(ctx context.Context, fs models.FS, f models.File) bool {
	const (
		unsetString = "unset"
		unsetNumber = -1
	)

	vf, ok := f.(*models.VideoFile)
	if !ok {
		return true
	}

	interactive := false
	if _, err := fs.Lstat(GetFunscriptPath(vf.Base().Path)); err == nil {
		interactive = true
	}

	return vf.VideoCodec == unsetString || vf.AudioCodec == unsetString ||
		vf.Format == unsetString || vf.Width == unsetNumber ||
		vf.Height == unsetNumber || vf.FrameRate == unsetNumber ||
		vf.Duration == unsetNumber ||
		vf.BitRate == unsetNumber || interactive != vf.Interactive
}
