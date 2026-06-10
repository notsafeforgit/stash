package image

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dsoprea/go-exif/v3"
	jis "github.com/dsoprea/go-jpeg-image-structure/v2"
	log "github.com/dsoprea/go-logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyOrientationRotation(t *testing.T) {
	// Two cycles of four orientations each: the non-flipped {1,6,3,8}
	// and the horizontally-flipped {2,5,4,7}. Each entry is
	// {start, expectedAfterCW, expectedAfterCCW, expectedAfterFlip}.
	cases := []struct {
		current uint16
		cw      uint16
		ccw     uint16
		flip    uint16
	}{
		// Non-flipped cycle
		{1, 6, 8, 3},
		{6, 3, 1, 8},
		{3, 8, 6, 1},
		{8, 1, 3, 6},
		// Horizontally-flipped cycle
		{2, 5, 7, 4},
		{5, 4, 2, 7},
		{4, 7, 5, 2},
		{7, 2, 4, 5},
	}

	for _, c := range cases {
		gotCW, err := applyOrientationRotation(c.current, RotateCW)
		assert.NoError(t, err)
		assert.Equal(t, c.cw, gotCW, "CW from %d", c.current)

		gotCCW, err := applyOrientationRotation(c.current, RotateCCW)
		assert.NoError(t, err)
		assert.Equal(t, c.ccw, gotCCW, "CCW from %d", c.current)

		gotFlip, err := applyOrientationRotation(c.current, RotateFlip)
		assert.NoError(t, err)
		assert.Equal(t, c.flip, gotFlip, "Flip from %d", c.current)

		// Three CWs is the same as one CCW.
		o := c.current
		for i := 0; i < 3; i++ {
			o, err = applyOrientationRotation(o, RotateCW)
			require.NoError(t, err)
		}
		assert.Equal(t, c.ccw, o, "3xCW == CCW from %d", c.current)

		// Two CWs is the same as one Flip.
		o = c.current
		for i := 0; i < 2; i++ {
			o, err = applyOrientationRotation(o, RotateCW)
			require.NoError(t, err)
		}
		assert.Equal(t, c.flip, o, "2xCW == Flip from %d", c.current)

		// Four CWs returns to identity.
		o = c.current
		for i := 0; i < 4; i++ {
			o, err = applyOrientationRotation(o, RotateCW)
			require.NoError(t, err)
		}
		assert.Equal(t, c.current, o, "4xCW returns to start from %d", c.current)
	}
}

func TestApplyOrientationRotation_InvalidInputs(t *testing.T) {
	_, err := applyOrientationRotation(0, RotateCW)
	assert.Error(t, err)
	_, err = applyOrientationRotation(9, RotateCW)
	assert.Error(t, err)
	_, err = applyOrientationRotation(1, RotateDirection(99))
	assert.Error(t, err)
}

// buildExifJPEG synthesises a minimal JPEG that the dsoprea parser will
// accept: SOI + APP1/Exif (containing a TIFF block with a single
// Orientation tag in IFD0) + SOS (with one byte of fake compressed data,
// since the splitter looks for a real marker after scan data) + EOI.
func buildExifJPEG(byteOrder binary.ByteOrder, orientation uint16) []byte {
	tiff := new(bytes.Buffer)
	if byteOrder == binary.LittleEndian {
		tiff.WriteString("II")
	} else {
		tiff.WriteString("MM")
	}
	binary.Write(tiff, byteOrder, uint16(0x002A))
	binary.Write(tiff, byteOrder, uint32(8)) // IFD0 starts immediately after the 8-byte header

	binary.Write(tiff, byteOrder, uint16(1))      // 1 entry
	binary.Write(tiff, byteOrder, uint16(0x0112)) // tag = Orientation
	binary.Write(tiff, byteOrder, uint16(3))      // type = SHORT
	binary.Write(tiff, byteOrder, uint32(1))      // count = 1
	binary.Write(tiff, byteOrder, orientation)    // value (2 bytes)
	binary.Write(tiff, byteOrder, uint16(0))      // padding to fill 4-byte value slot
	binary.Write(tiff, byteOrder, uint32(0))      // next IFD offset (none)

	app1 := new(bytes.Buffer)
	app1.WriteString("Exif")
	app1.Write([]byte{0, 0})
	app1.Write(tiff.Bytes())

	out := new(bytes.Buffer)
	out.Write([]byte{0xFF, 0xD8}) // SOI
	out.Write([]byte{0xFF, 0xE1}) // APP1 marker
	binary.Write(out, binary.BigEndian, uint16(2+app1.Len()))
	out.Write(app1.Bytes())
	out.Write([]byte{0xFF, 0xDA, 0x00, 0x02}) // SOS marker + 2-byte length (empty payload)
	out.Write([]byte{0x00})                   // 1 byte of "compressed" data so the scanner sees scan content
	out.Write([]byte{0xFF, 0xD9})             // EOI
	return out.Bytes()
}

// buildBareJPEG builds a JPEG with no APP1/Exif segment — just SOI, SOS
// with a tiny scan body, and EOI. Used to test the "add EXIF if stripped"
// behaviour.
func buildBareJPEG() []byte {
	out := new(bytes.Buffer)
	out.Write([]byte{0xFF, 0xD8})             // SOI
	out.Write([]byte{0xFF, 0xDA, 0x00, 0x02}) // SOS marker + 2-byte length (empty payload)
	out.Write([]byte{0x00})                   // 1 byte of scan data
	out.Write([]byte{0xFF, 0xD9})             // EOI
	return out.Bytes()
}

func writeTempJPEG(t *testing.T, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.jpg")
	require.NoError(t, os.WriteFile(p, data, 0o644))
	return p
}

func readOrientation(t *testing.T, path string) uint16 {
	t.Helper()
	parser := jis.NewJpegMediaParser()
	mc, err := parser.ParseFile(path)
	require.NoError(t, err)
	sl := mc.(*jis.SegmentList)

	rootIfd, _, err := sl.Exif()
	require.NoError(t, err)

	entries, err := rootIfd.FindTagWithName("Orientation")
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	v, err := entries[0].Value()
	require.NoError(t, err)
	vs, ok := v.([]uint16)
	require.True(t, ok)
	require.NotEmpty(t, vs)
	return vs[0]
}

func TestPatchJPEGOrientation_RoundTrip(t *testing.T) {
	for _, bo := range []struct {
		name      string
		byteOrder binary.ByteOrder
	}{
		{"little-endian", binary.LittleEndian},
		{"big-endian", binary.BigEndian},
	} {
		t.Run(bo.name, func(t *testing.T) {
			path := writeTempJPEG(t, buildExifJPEG(bo.byteOrder, 1))

			// 1 -> CW -> 6 (raw camera 90° rotation; raw dims now flipped).
			res, err := PatchJPEGOrientation(path, RotateCW, PatchJPEGOrientationOptions{})
			require.NoError(t, err)
			assert.Equal(t, uint16(1), res.OldOrientation)
			assert.Equal(t, uint16(6), res.NewOrientation)
			assert.True(t, res.DimensionsSwapped)

			assert.Equal(t, uint16(6), readOrientation(t, path))

			// 6 -> CW -> 3 (180° from start; back to non-flipped dims).
			res, err = PatchJPEGOrientation(path, RotateCW, PatchJPEGOrientationOptions{})
			require.NoError(t, err)
			assert.Equal(t, uint16(6), res.OldOrientation)
			assert.Equal(t, uint16(3), res.NewOrientation)
			assert.True(t, res.DimensionsSwapped)

			// 3 -> Flip -> 1 (back to identity).
			res, err = PatchJPEGOrientation(path, RotateFlip, PatchJPEGOrientationOptions{})
			require.NoError(t, err)
			assert.Equal(t, uint16(3), res.OldOrientation)
			assert.Equal(t, uint16(1), res.NewOrientation)
			assert.False(t, res.DimensionsSwapped)

			assert.Equal(t, uint16(1), readOrientation(t, path))
		})
	}
}

func TestPatchJPEGOrientation_NotAJPEG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.jpg")
	require.NoError(t, os.WriteFile(path, []byte("not a jpeg"), 0o644))
	_, err := PatchJPEGOrientation(path, RotateCW, PatchJPEGOrientationOptions{})
	assert.ErrorIs(t, err, ErrUnsupportedRotation)
}

// JPEGs without any APP1/Exif segment should now succeed: the patch adds
// a fresh EXIF segment with just an Orientation tag, treating the starting
// orientation as 1 (the EXIF default).
func TestPatchJPEGOrientation_NoExifSegmentAddsExif(t *testing.T) {
	path := writeTempJPEG(t, buildBareJPEG())

	res, err := PatchJPEGOrientation(path, RotateCW, PatchJPEGOrientationOptions{})
	require.NoError(t, err)
	assert.Equal(t, uint16(1), res.OldOrientation)
	assert.Equal(t, uint16(6), res.NewOrientation)
	assert.True(t, res.DimensionsSwapped)

	assert.Equal(t, uint16(6), readOrientation(t, path))
}

// JPEGs with an EXIF segment but no Orientation tag should also succeed:
// the patch adds an Orientation tag, treating the starting orientation
// as 1.
func TestPatchJPEGOrientation_NoOrientationTagAddsTag(t *testing.T) {
	tiff := new(bytes.Buffer)
	tiff.WriteString("II")
	binary.Write(tiff, binary.LittleEndian, uint16(0x002A))
	binary.Write(tiff, binary.LittleEndian, uint32(8))
	binary.Write(tiff, binary.LittleEndian, uint16(0)) // 0 entries
	binary.Write(tiff, binary.LittleEndian, uint32(0)) // next IFD = 0

	app1 := new(bytes.Buffer)
	app1.WriteString("Exif")
	app1.Write([]byte{0, 0})
	app1.Write(tiff.Bytes())

	out := new(bytes.Buffer)
	out.Write([]byte{0xFF, 0xD8})
	out.Write([]byte{0xFF, 0xE1})
	binary.Write(out, binary.BigEndian, uint16(2+app1.Len()))
	out.Write(app1.Bytes())
	out.Write([]byte{0xFF, 0xDA, 0x00, 0x02})
	out.Write([]byte{0x00})
	out.Write([]byte{0xFF, 0xD9})

	path := writeTempJPEG(t, out.Bytes())

	res, err := PatchJPEGOrientation(path, RotateCW, PatchJPEGOrientationOptions{})
	require.NoError(t, err)
	assert.Equal(t, uint16(1), res.OldOrientation)
	assert.Equal(t, uint16(6), res.NewOrientation)

	assert.Equal(t, uint16(6), readOrientation(t, path))
}

// PreserveMTime: true should leave the file's mtime untouched, even
// though the rest of the file has been rewritten via temp + rename.
func TestPatchJPEGOrientation_PreservesMTime(t *testing.T) {
	path := writeTempJPEG(t, buildExifJPEG(binary.LittleEndian, 1))

	// Backdate the file so we can distinguish "preserved" from "now".
	original := time.Date(2010, 6, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(path, original, original))

	_, err := PatchJPEGOrientation(path, RotateCW, PatchJPEGOrientationOptions{PreserveMTime: true})
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	// Most filesystems store mtime at sub-second resolution; require an
	// exact match to catch any accidental drift.
	assert.True(t, info.ModTime().Equal(original),
		"expected mtime preserved as %v, got %v", original, info.ModTime())
}

// PreserveMTime: false (the zero-value default) should update mtime to
// the time of the rewrite — the previous mtime is lost.
func TestPatchJPEGOrientation_DefaultUpdatesMTime(t *testing.T) {
	path := writeTempJPEG(t, buildExifJPEG(binary.LittleEndian, 1))

	original := time.Date(2010, 6, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(path, original, original))

	_, err := PatchJPEGOrientation(path, RotateCW, PatchJPEGOrientationOptions{})
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.False(t, info.ModTime().Equal(original),
		"expected mtime to change from %v, but it stayed", original)
}

func TestErrUnsupportedRotation_IsSentinel(t *testing.T) {
	// Sanity: errors.Is unwraps fmt.Errorf("...: %w", err) chains.
	wrapped := fmt.Errorf("rotating: %w", ErrUnsupportedRotation)
	assert.True(t, errors.Is(wrapped, ErrUnsupportedRotation))
}

// Sanity-check our use of dsoprea's log.Is for the ErrNoExif/ErrTagNotFound
// sentinels — if a future dsoprea version changes its wrapping behaviour,
// readCurrentOrientation would silently start returning errors.
func TestDsopreaErrorSentinels(t *testing.T) {
	assert.True(t, log.Is(exif.ErrNoExif, exif.ErrNoExif))
	assert.True(t, log.Is(exif.ErrTagNotFound, exif.ErrTagNotFound))
}
