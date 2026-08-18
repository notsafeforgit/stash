package video

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stashapp/stash/pkg/ffmpeg"
)

// RotationDirection describes a metadata-only change to the displayed video
// orientation. Rotating composes with the current container rotation;
// clearing removes the transform entirely.
type RotationDirection int

const (
	RotateCW RotationDirection = iota
	RotateCCW
	RotateClear
)

// ErrUnsupportedRotation indicates that a video cannot be safely updated by
// one of the stream-copy metadata remuxes used here.
var ErrUnsupportedRotation = errors.New("video container does not support rotation metadata editing")

type rotationStorage int

const (
	rotationStorageMatroskaTag rotationStorage = iota
	rotationStorageDisplayMatrix
)

type rotationContainer struct {
	storage       rotationStorage
	outputFormat  string
	tempExtension string
}

type rotationRemuxer interface {
	Generate(context.Context, ffmpeg.Args) error
}

type rotationProber interface {
	NewVideoFile(string) (*ffmpeg.VideoFile, error)
}

// RotationPatch is a staged stream-copy remux. Apply swaps the staged file
// into place while retaining the original beside it; Commit removes that
// backup, and Rollback restores it. This lets the API keep the filesystem
// mutation and its database transaction consistent.
type RotationPatch struct {
	Path            string
	StagedPath      string
	CurrentRotation int64
	Rotation        int64

	originalSize    int64
	originalModTime int64
	originalInfo    os.FileInfo
	backupPath      string
	applied         bool
	committed       bool
}

// StageRotationMetadata remuxes a supported video without re-encoding,
// changing only the first video stream's rotation metadata. Matroska stores a
// ROTATE tag, while MP4/M4V/MOV store a display matrix. The original file is
// untouched until Apply.
func StageRotationMetadata(
	ctx context.Context,
	encoder rotationRemuxer,
	probe rotationProber,
	path string,
	direction RotationDirection,
) (*RotationPatch, error) {
	if encoder == nil || probe == nil {
		return nil, errors.New("ffmpeg and ffprobe must be configured")
	}

	before, err := probe.NewVideoFile(path)
	if err != nil {
		return nil, fmt.Errorf("probing rotation metadata for %q: %w", path, err)
	}
	container, err := rotationContainerFor(path, before.Container)
	if err != nil {
		return nil, err
	}
	if before.VideoStream == nil {
		return nil, fmt.Errorf("%w: %q has no video stream", ErrUnsupportedRotation, path)
	}

	current := canonicalRotation(before.Rotation)
	target, clearMetadata, err := targetRotation(current, direction)
	if err != nil {
		return nil, err
	}

	linkInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("statting %q: %w", path, err)
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: symlinked videos cannot be replaced safely", ErrUnsupportedRotation)
	}
	originalInfo, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("statting %q: %w", path, err)
	}

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	temp, err := os.CreateTemp(dir, "."+base+".rotate-*"+container.tempExtension)
	if err != nil {
		return nil, fmt.Errorf("creating rotation staging file beside %q: %w", path, err)
	}
	stagedPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(stagedPath)
		return nil, fmt.Errorf("closing rotation staging file: %w", err)
	}

	args := ffmpeg.Args{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
	}
	if container.storage == rotationStorageDisplayMatrix {
		args = append(args, "-display_rotation:v:0", strconv.FormatInt(signedRotation(target), 10))
	}
	args = append(args,
		"-i", path,
		"-map", "0",
		"-map_metadata", "0",
		"-map_chapters", "0",
		"-copy_unknown",
		"-c", "copy",
	)
	if container.storage == rotationStorageMatroskaTag {
		metadata := "ROTATE="
		if !clearMetadata {
			metadata += strconv.FormatInt(target, 10)
		}
		args = append(args, "-metadata:s:v:0", metadata)
	}
	args = append(args,
		"-f", container.outputFormat,
		stagedPath,
	)
	if err := encoder.Generate(ctx, args); err != nil {
		_ = os.Remove(stagedPath)
		return nil, fmt.Errorf("remuxing rotation metadata for %q: %w", path, err)
	}

	// A replacement inode should retain the source's permission bits.
	if err := os.Chmod(stagedPath, originalInfo.Mode()); err != nil {
		_ = os.Remove(stagedPath)
		return nil, fmt.Errorf("preserving permissions for %q: %w", path, err)
	}

	after, err := probe.NewVideoFile(stagedPath)
	if err != nil {
		_ = os.Remove(stagedPath)
		return nil, fmt.Errorf("verifying rotation metadata for %q: %w", path, err)
	}
	if after.VideoStream == nil {
		_ = os.Remove(stagedPath)
		return nil, fmt.Errorf("verifying rotation metadata for %q: remux has no video stream", path)
	}
	if canonicalRotation(after.Rotation) != target {
		_ = os.Remove(stagedPath)
		return nil, fmt.Errorf("verifying rotation metadata for %q: got %d, want %d", path, after.Rotation, target)
	}
	if clearMetadata && container.storage == rotationStorageMatroskaTag && after.VideoStream.Tags.Rotate != "" {
		_ = os.Remove(stagedPath)
		return nil, fmt.Errorf("verifying cleared rotation metadata for %q: ROTATE tag remains", path)
	}

	return &RotationPatch{
		Path:            path,
		StagedPath:      stagedPath,
		CurrentRotation: current,
		Rotation:        target,
		originalSize:    originalInfo.Size(),
		originalModTime: originalInfo.ModTime().UnixNano(),
		originalInfo:    originalInfo,
	}, nil
}

// Apply atomically replaces the original path with the staged remux after
// verifying that another process has not changed the source since staging.
func (p *RotationPatch) Apply() error {
	if p == nil {
		return errors.New("rotation patch is nil")
	}
	if p.applied || p.committed {
		return errors.New("rotation patch was already applied")
	}

	info, err := os.Stat(p.Path)
	if err != nil {
		return fmt.Errorf("statting source before rotation swap: %w", err)
	}
	if !os.SameFile(p.originalInfo, info) || info.Size() != p.originalSize || info.ModTime().UnixNano() != p.originalModTime {
		return fmt.Errorf("source changed while rotation metadata was being prepared")
	}

	p.backupPath = p.StagedPath + ".original"
	if err := os.Rename(p.Path, p.backupPath); err != nil {
		return fmt.Errorf("backing up source before rotation swap: %w", err)
	}
	p.applied = true
	if err := os.Rename(p.StagedPath, p.Path); err != nil {
		restoreErr := os.Rename(p.backupPath, p.Path)
		if restoreErr != nil {
			return fmt.Errorf("installing rotated video: %w (also failed to restore original: %v)", err, restoreErr)
		}
		p.applied = false
		return fmt.Errorf("installing rotated video: %w", err)
	}
	return nil
}

// Commit makes an applied patch permanent by deleting its retained original.
func (p *RotationPatch) Commit() error {
	if p == nil || !p.applied || p.committed {
		return nil
	}
	p.committed = true
	p.applied = false
	if err := os.Remove(p.backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing original video backup %q: %w", p.backupPath, err)
	}
	return nil
}

// Rollback restores the original after Apply, or discards an unapplied staged
// remux. It is safe to call from a defer after Commit.
func (p *RotationPatch) Rollback() error {
	if p == nil || p.committed {
		return nil
	}
	if !p.applied {
		if err := os.Remove(p.StagedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("discarding staged rotated video %q: %w", p.StagedPath, err)
		}
		return nil
	}

	if err := os.Remove(p.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing uncommitted rotated video %q: %w", p.Path, err)
	}
	if err := os.Rename(p.backupPath, p.Path); err != nil {
		return fmt.Errorf("restoring original video %q from %q: %w", p.Path, p.backupPath, err)
	}
	p.applied = false
	if err := os.Remove(p.StagedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("discarding staged rotated video %q after restore: %w", p.StagedPath, err)
	}
	return nil
}

func targetRotation(current int64, direction RotationDirection) (rotation int64, clearMetadata bool, err error) {
	switch direction {
	case RotateCW:
		// FFmpeg interprets display-rotation metadata as a
		// counter-clockwise angle, so a visible clockwise turn subtracts.
		return canonicalRotation(current - 90), false, nil
	case RotateCCW:
		return canonicalRotation(current + 90), false, nil
	case RotateClear:
		return 0, true, nil
	default:
		return 0, false, fmt.Errorf("invalid video rotation direction %d", direction)
	}
}

func canonicalRotation(rotation int64) int64 {
	rotation %= 360
	if rotation < 0 {
		rotation += 360
	}
	return rotation
}

func signedRotation(rotation int64) int64 {
	rotation = canonicalRotation(rotation)
	if rotation > 180 {
		rotation -= 360
	}
	return rotation
}

func rotationContainerFor(path, probedContainer string) (rotationContainer, error) {
	extension := strings.ToLower(filepath.Ext(path))
	switch extension {
	case ".mkv":
		if isMatroskaContainer(probedContainer) {
			return rotationContainer{
				storage:       rotationStorageMatroskaTag,
				outputFormat:  "matroska",
				tempExtension: ".mkv",
			}, nil
		}
	case ".mp4", ".m4v":
		if isISOBaseMediaContainer(probedContainer) {
			return rotationContainer{
				storage:       rotationStorageDisplayMatrix,
				outputFormat:  "mp4",
				tempExtension: extension,
			}, nil
		}
	case ".mov":
		if isISOBaseMediaContainer(probedContainer) {
			return rotationContainer{
				storage:       rotationStorageDisplayMatrix,
				outputFormat:  "mov",
				tempExtension: extension,
			}, nil
		}
	}

	return rotationContainer{}, fmt.Errorf(
		"%w: %q must be an MKV, MP4, M4V, or MOV file with a matching container",
		ErrUnsupportedRotation,
		path,
	)
}

func isMatroskaContainer(container string) bool {
	for _, name := range strings.Split(container, ",") {
		if strings.EqualFold(strings.TrimSpace(name), "matroska") {
			return true
		}
	}
	return false
}

func isISOBaseMediaContainer(container string) bool {
	for _, name := range strings.Split(container, ",") {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "mov", "mp4":
			return true
		}
	}
	return false
}
