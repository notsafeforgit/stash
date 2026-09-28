package previewimage

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func legacyCompactFixture(t *testing.T, fullRange, thumbRange DynamicRange) (Store, string, *Manifest) {
	t.Helper()
	store := Store{Root: t.TempDir()}
	key := CoverKey("legacy")
	dir, err := store.directory(1, "cover", key)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0755))
	asset := func(data, ext, mime string, dr DynamicRange, width int) Variant {
		name := fmt.Sprintf("%x%s", sha256.Sum256([]byte(data)), ext)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(data), 0600))
		return Variant{File: name, MIMEType: mime, DynamicRange: dr, Width: width, Height: width / 2}
	}
	m := &Manifest{Version: RecipeVersion, Key: key, At: 12.345,
		Variants:  []Variant{asset("full jpeg", ".jpg", "image/jpeg", SDR, 1280), asset("full avif", ".avif", "image/avif", fullRange, 1280)},
		Thumbnail: []Variant{asset("thumb jpeg", ".jpg", "image/jpeg", SDR, 640), asset("thumb avif", ".avif", "image/avif", thumbRange, 640)}}
	// Seed the previous format directly: current publication removes these
	// duplicates, while older on-disk manifests must still load unchanged.
	require.NoError(t, writeManifest(dir, *m))
	m, err = store.Load(1, "cover", key)
	require.NoError(t, err)
	return store, dir, m
}

func TestCompactPreviewJPEGs(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		full, thumbnail       DynamicRange
		fullCount, thumbCount int
		bytes                 int64
	}{
		{"SDR", SDR, SDR, 1, 1, 19},
		{"adaptive", Adaptive, Adaptive, 1, 1, 19},
		{"plain HDR needs JPEG", HDR, HDR, 2, 2, 0},
		{"independent sizes", Adaptive, HDR, 1, 2, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, dir, before := legacyCompactFixture(t, tc.full, tc.thumbnail)
			original, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "unmanaged.jpg"), []byte("keep"), 0600))
			bytes, err := store.Compact(1, "cover", before.Key, true)
			require.NoError(t, err)
			require.Equal(t, tc.bytes, bytes)
			dryRun, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			require.NoError(t, err)
			require.Equal(t, original, dryRun)
			require.FileExists(t, filepath.Join(dir, before.Variants[0].File))
			bytes, err = store.Compact(1, "cover", before.Key, false)
			require.NoError(t, err)
			require.Equal(t, tc.bytes, bytes)
			after, err := store.Load(1, "cover", before.Key)
			require.NoError(t, err)
			require.Len(t, after.Variants, tc.fullCount)
			require.Len(t, after.Thumbnail, tc.thumbCount)
			require.Equal(t, before.At, after.At)
			require.Equal(t, before.Key, after.Key)
			if tc.bytes > 0 {
				require.NotEqual(t, before.Revision, after.Revision)
			}
			require.FileExists(t, filepath.Join(dir, "unmanaged.jpg"))
			bytes, err = store.Compact(1, "cover", before.Key, false)
			require.NoError(t, err)
			require.Zero(t, bytes, "repeated cleanup must be harmless")
		})
	}
}

func TestCompactPreservesFallbackOnCorruptAVIF(t *testing.T) {
	store, dir, before := legacyCompactFixture(t, Adaptive, SDR)
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, before.Thumbnail[1].File), []byte("corrupt avif"), 0600))
	for _, dryRun := range []bool{true, false} {
		bytes, err := store.Compact(1, "cover", before.Key, dryRun)
		require.ErrorContains(t, err, "integrity check")
		require.Zero(t, bytes)
		after, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		require.NoError(t, err)
		require.Equal(t, manifest, after)
		require.FileExists(t, filepath.Join(dir, before.Variants[0].File))
		require.FileExists(t, filepath.Join(dir, before.Thumbnail[0].File))
	}
}

func TestCompactReclaimsJPEGsLeftByRegeneration(t *testing.T) {
	store, dir, old := legacyCompactFixture(t, Adaptive, SDR)
	stage := t.TempDir()
	result := &Result{Directory: stage, Variants: []Variant{{"new.jpg", "image/jpeg", SDR, 64, 32}, {"new.avif", "image/avif", SDR, 64, 32}}}
	for _, v := range result.Variants {
		require.NoError(t, os.WriteFile(filepath.Join(stage, v.File), []byte(v.File), 0600))
	}
	require.NoError(t, store.Publish(1, "cover", old.Key, old.At, result))
	m, err := store.Load(1, "cover", old.Key)
	require.NoError(t, err)
	require.Len(t, m.Variants, 1)
	require.Equal(t, "image/avif", m.Variants[0].MIMEType)
	require.FileExists(t, filepath.Join(stage, "new.jpg"), "compatibility JPEG is temporary, not published")
	bytes, err := store.Compact(1, "cover", old.Key, false)
	require.NoError(t, err)
	require.Equal(t, int64(19), bytes)
	require.NoFileExists(t, filepath.Join(dir, old.Variants[0].File))
	require.NoFileExists(t, filepath.Join(dir, old.Thumbnail[0].File))
}
