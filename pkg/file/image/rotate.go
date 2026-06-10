package image

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/dsoprea/go-exif/v3"
	jis "github.com/dsoprea/go-jpeg-image-structure/v2"
	log "github.com/dsoprea/go-logging"
)

// RotateDirection describes a 90°-multiple rotation request from a user. The
// direction is applied to the *displayed* orientation of the image — i.e.
// "RotateCW" rotates the visible image 90° clockwise, regardless of what
// rotation the EXIF tag was already specifying.
type RotateDirection int

const (
	RotateCW RotateDirection = iota
	RotateCCW
	RotateFlip
)

// ErrUnsupportedRotation indicates the file format does not support EXIF
// orientation rewrites (i.e. is not a JPEG, or its EXIF block is malformed
// in a way we can't recover from).
var ErrUnsupportedRotation = errors.New("image does not support EXIF orientation rotation")

// orientationTagName is the EXIF IFD0 tag name dsoprea uses for the
// Orientation field (numeric tag 0x0112).
const orientationTagName = "Orientation"

// applyOrientationRotation maps `current` (1–8) and a user-facing direction
// to the new EXIF Orientation value that should be written back. The eight
// EXIF orientations split into two cycles: a non-flipped cycle
// 1 → 6 → 3 → 8 → 1 (rotating clockwise) and a horizontally-flipped cycle
// 2 → 5 → 4 → 7 → 2. CCW is the reverse and Flip is two CW steps.
func applyOrientationRotation(current uint16, dir RotateDirection) (uint16, error) {
	cwTable := map[uint16]uint16{
		1: 6, 6: 3, 3: 8, 8: 1, // non-flipped cycle
		2: 5, 5: 4, 4: 7, 7: 2, // flipped cycle
	}
	if _, ok := cwTable[current]; !ok {
		return 0, fmt.Errorf("invalid current orientation %d", current)
	}

	steps := 1
	switch dir {
	case RotateCW:
		steps = 1
	case RotateCCW:
		steps = 3 // three CW steps == one CCW step
	case RotateFlip:
		steps = 2
	default:
		return 0, fmt.Errorf("invalid rotation direction %d", dir)
	}

	o := current
	for i := 0; i < steps; i++ {
		o = cwTable[o]
	}
	return o, nil
}

// RotateResult describes the outcome of a successful EXIF Orientation patch.
// `DimensionsSwapped` is true when the stored width and height of the file
// should be swapped to reflect the new displayed orientation (i.e. one of
// old/new puts the raw pixels on their side and the other does not).
type RotateResult struct {
	OldOrientation    uint16
	NewOrientation    uint16
	DimensionsSwapped bool
}

// PatchJPEGOrientationOptions configures a `PatchJPEGOrientation` call.
type PatchJPEGOrientationOptions struct {
	// PreserveMTime, when true, restores the original file mtime after the
	// atomic rewrite. Useful for scraped images whose mtime carries
	// meaningful upload-time information that shouldn't be lost just
	// because the user fixed the displayed orientation.
	PreserveMTime bool
}

// PatchJPEGOrientation rewrites the EXIF Orientation tag of the JPEG file at
// `path` to apply the supplied rotation `dir`. If the file has no EXIF
// segment, an APP1/Exif segment is added with a single Orientation tag.
// Pixel data is untouched.
//
// The file is rewritten via temp-file + atomic rename, so a crash mid-write
// won't leave the original truncated. Returns ErrUnsupportedRotation if the
// file is not a JPEG.
func PatchJPEGOrientation(path string, dir RotateDirection, opts PatchJPEGOrientationOptions) (RotateResult, error) {
	if err := requireJPEG(path); err != nil {
		return RotateResult{}, err
	}

	// Capture the original mtime up-front so we can restore it after the
	// rename. We do this before any other I/O so a non-existent file
	// surfaces here rather than later in the parser.
	var origMTime time.Time
	if opts.PreserveMTime {
		info, err := os.Stat(path)
		if err != nil {
			return RotateResult{}, fmt.Errorf("stat for mtime: %w", err)
		}
		origMTime = info.ModTime()
	}

	parser := jis.NewJpegMediaParser()
	mc, err := parser.ParseFile(path)
	if err != nil {
		return RotateResult{}, fmt.Errorf("%w: parsing JPEG: %v", ErrUnsupportedRotation, err)
	}
	sl, ok := mc.(*jis.SegmentList)
	if !ok {
		return RotateResult{}, fmt.Errorf("%w: unexpected media context type %T", ErrUnsupportedRotation, mc)
	}

	current, err := readCurrentOrientation(sl)
	if err != nil {
		return RotateResult{}, err
	}

	next, err := applyOrientationRotation(current, dir)
	if err != nil {
		return RotateResult{}, err
	}

	rootIb, err := sl.ConstructExifBuilder()
	if err != nil {
		return RotateResult{}, fmt.Errorf("constructing EXIF builder: %w", err)
	}

	if err := rootIb.SetStandardWithName(orientationTagName, []uint16{next}); err != nil {
		return RotateResult{}, fmt.Errorf("setting orientation tag: %w", err)
	}

	if err := sl.SetExif(rootIb); err != nil {
		return RotateResult{}, fmt.Errorf("applying EXIF segment: %w", err)
	}

	if err := writeJPEGAtomic(path, sl); err != nil {
		return RotateResult{}, err
	}

	if opts.PreserveMTime {
		// Pass time.Now() for atime so we don't backdate access time —
		// the user only cares about the mtime field.
		if err := os.Chtimes(path, time.Now(), origMTime); err != nil {
			return RotateResult{}, fmt.Errorf("restoring mtime on %q: %w", path, err)
		}
	}

	return RotateResult{
		OldOrientation:    current,
		NewOrientation:    next,
		DimensionsSwapped: IsOrientationDimensionsFlipped(int(current)) != IsOrientationDimensionsFlipped(int(next)),
	}, nil
}

// requireJPEG verifies the file at `path` starts with a JPEG SOI marker.
// dsoprea's parser will also reject non-JPEGs but its error type is opaque;
// checking here lets us return the typed sentinel for the common case.
func requireJPEG(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %q: %w", path, err)
	}
	defer f.Close()

	var soi [2]byte
	if _, err := io.ReadFull(f, soi[:]); err != nil {
		return fmt.Errorf("%w: reading SOI: %v", ErrUnsupportedRotation, err)
	}
	if soi[0] != 0xFF || soi[1] != 0xD8 {
		return fmt.Errorf("%w: not a JPEG (bad SOI)", ErrUnsupportedRotation)
	}
	return nil
}

// readCurrentOrientation pulls the existing Orientation value out of the
// JPEG's EXIF segment. Returns 1 (the EXIF default for "no rotation") if
// either the EXIF segment or the Orientation tag is missing.
func readCurrentOrientation(sl *jis.SegmentList) (uint16, error) {
	rootIfd, _, err := sl.Exif()
	if err != nil {
		if log.Is(err, exif.ErrNoExif) {
			return 1, nil
		}
		return 0, fmt.Errorf("reading EXIF: %w", err)
	}

	entries, err := rootIfd.FindTagWithName(orientationTagName)
	if err != nil {
		if log.Is(err, exif.ErrTagNotFound) {
			return 1, nil
		}
		return 0, fmt.Errorf("finding orientation tag: %w", err)
	}
	if len(entries) == 0 {
		return 1, nil
	}

	v, err := entries[0].Value()
	if err != nil {
		return 0, fmt.Errorf("reading orientation tag: %w", err)
	}
	vs, ok := v.([]uint16)
	if !ok || len(vs) == 0 {
		return 0, fmt.Errorf("orientation tag has unexpected value %T", v)
	}
	return vs[0], nil
}

// writeJPEGAtomic serialises `sl` to a temp file in the same directory as
// `path`, fsyncs it, then renames it over `path`. The original file's mode
// is preserved.
func writeJPEGAtomic(path string, sl *jis.SegmentList) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".rotate-*.jpg")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := sl.Write(tmp); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp: %w", err)
	}

	if info, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmpPath, info.Mode())
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming temp over %q: %w", path, err)
	}
	cleanup = false
	return nil
}
