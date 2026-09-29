package previewimage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Publication and cleanup must not race within an entry. Bound the lock table
// independently of library size; separate entries can still publish in parallel.
var publicationLocks [64]sync.Mutex

func publicationLock(dir string) *sync.Mutex {
	key := sha256.Sum256([]byte(dir))
	return &publicationLocks[int(key[0])%len(publicationLocks)]
}

// Keep old JPEG-only/HDR+JPEG catalogs readable until regeneration supplies a
// display-safe AVIF. An SDR-base gain map is itself a complete SDR fallback.
func avifRenditions(variants []Variant) []Variant {
	if len(variants) == 0 {
		return nil
	}
	hasFallback := false
	for _, v := range variants {
		hasFallback = hasFallback || (v.MIMEType == "image/avif" && v.DynamicRange != HDR)
	}
	ret := make([]Variant, 0, len(variants))
	for _, v := range variants {
		if !hasFallback || v.MIMEType != "image/jpeg" {
			ret = append(ret, v)
		}
	}
	return ret
}

func writeManifest(dir string, manifest Manifest) error {
	manifest.Revision = ""
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	manifest.Revision = fmt.Sprintf("%x", sha256.Sum256(data))
	data, err = json.Marshal(manifest)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".manifest-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(dir, "manifest.json"))
}

// Compact removes redundant JPEGs and unreferenced AVIFs from an existing
// generated entry without decoding the original or re-encoding AVIF. It retains
// JPEGs when an old plain HDR AVIF still needs them for SDR. Dry runs inspect
// exactly the same files.
func (s Store) Compact(entityID int, kind, key string, dryRun bool) (int64, error) {
	dir, err := s.directory(entityID, kind, key)
	if err != nil {
		return 0, err
	}
	lock := publicationLock(dir)
	lock.Lock()
	defer lock.Unlock()
	m, err := s.Load(entityID, kind, key)
	if err != nil {
		return 0, err
	}
	variants, thumbnails := avifRenditions(m.Variants), avifRenditions(m.Thumbnail)
	changed := len(variants) != len(m.Variants) || len(thumbnails) != len(m.Thumbnail)
	// Check the immutable assets' content hashes before discarding a fallback.
	// A truncated/corrupt AVIF must leave the manifest and older renditions intact.
	for _, group := range [][]Variant{variants, thumbnails} {
		for _, v := range group {
			if v.MIMEType == "image/avif" {
				if err := verifyAsset(dir, v.File); err != nil {
					return 0, err
				}
			}
		}
	}
	m.Variants, m.Thumbnail = variants, thumbnails
	if changed && !dryRun {
		if err := writeManifest(dir, *m); err != nil {
			return 0, err
		}
	}
	keep := make(map[string]bool)
	for _, group := range [][]Variant{variants, thumbnails} {
		for _, v := range group {
			keep[v.File] = true
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var reclaimed int64
	for _, entry := range entries {
		name := entry.Name()
		ext := filepath.Ext(name)
		if keep[name] || !entry.Type().IsRegular() || (ext != ".jpg" && ext != ".avif") || !hashedAssetName(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return reclaimed, err
		}
		if !dryRun {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return reclaimed, err
			}
		}
		reclaimed += info.Size()
	}
	return reclaimed, nil
}

func hashedAssetName(name string) bool {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if len(base) != 64 {
		return false
	}
	_, err := hex.DecodeString(base)
	return err == nil
}

func verifyAsset(dir, name string) error {
	if !hashedAssetName(name) {
		return fmt.Errorf("preview asset has no content hash: %s", name)
	}
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if fmt.Sprintf("%x", h.Sum(nil))+filepath.Ext(name) != name {
		return fmt.Errorf("preview asset failed integrity check: %s", name)
	}
	return nil
}
