package manager

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/previewimage"
)

var framePreviewSlots = make(chan struct{}, 2)
var ErrFramePreviewsBusy = errors.New("thumbnail previews are busy; try again shortly")

// SceneFramePreviews never changes the playing stream or persists artwork.
// Cancellation stops decoding; output exists only in bounded memory.
func (s *Manager) SceneFramePreviews(ctx context.Context, file *models.VideoFile, request previewimage.PickerRequest) ([]previewimage.PickerFrame, error) {
	select {
	case framePreviewSlots <- struct{}{}:
		defer func() { <-framePreviewSlots }()
	default:
		return nil, ErrFramePreviewsBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	lock := s.ReadLockManager.ReadLock(ctx, file.Path)
	defer lock.Cancel()
	before, err := previewimage.SourceKey(file.Path, "")
	if err != nil {
		return nil, err
	}
	descriptor, err := os.Open(file.Path)
	if err != nil {
		return nil, err
	}
	defer descriptor.Close()
	video, err := s.FFProbe.NewVideoFileFromOpen(lock, descriptor, file.Path)
	if err != nil {
		return nil, err
	}
	// UI/file duration may include a longer audio tail. Overview sampling must
	// stop at the video stream's end rather than seek into that empty tail.
	request.End = math.Min(request.End, video.VideoStreamDuration)
	frames, err := (previewimage.Encoder{FFmpeg: s.FFMpeg}).Picker(lock, video, request)
	if err != nil {
		return nil, err
	}
	if after, err := previewimage.SourceKey(file.Path, ""); err != nil || after != before {
		return nil, fmt.Errorf("video changed while extracting thumbnail previews")
	}
	return frames, nil
}
