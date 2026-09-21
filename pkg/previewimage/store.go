package previewimage

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
)

// RecipeVersion changes when encoding policy or manifest semantics change.
// Published entries are immutable. Old URLs can't silently serve a new image.
const RecipeVersion = 1

type Store struct{ Root string }

type Manifest struct {
	Version  int       `json:"version"`
	Key      string    `json:"key"`
	Revision string    `json:"revision"`
	At       float64   `json:"at"`
	Variants []Variant `json:"variants"`
	// Optional for manifests generated before card thumbnails were introduced.
	Thumbnail []Variant `json:"thumbnail,omitempty"`
}

// SourceKey binds an image to both its source file and the current artwork
// identity (a cover blob checksum or an exact, fractional marker timestamp).
// Replacing a file, changing the primary file or editing a cover in v2.5 makes
// a previous rendition unreachable without a database migration or reconciler.
func SourceKey(source, identity string) (string, error) {
	return sourceKey(RecipeVersion, source, identity)
}

func sourceKey(version int, source, identity string) (string, error) {
	stat, err := os.Stat(source)
	if err != nil {
		return "", err
	}
	if !stat.Mode().IsRegular() {
		return "", fmt.Errorf("preview source is not a regular file")
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%d\x00%d\x00%s", version, source, stat.Size(), stat.ModTime().UnixNano(), identity)))), nil
}

// CoverKey binds renditions to saved artwork, independently of the source
// video's availability. Source validity controls regeneration, not display.
func CoverKey(checksum string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d\x00cover\x00%s", RecipeVersion, checksum))))
}

// LegacyCoverTimestamp recovers the selection from the original v1 manifest
// only when both its source fingerprint and cover checksum still match. It
// deliberately ignores missing renditions and the current encoding recipe:
// those are precisely why a user may need to regenerate the cover.
func (s Store) LegacyCoverTimestamp(sceneID int, source, checksum string) (float64, error) {
	key, err := sourceKey(1, source, checksum)
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(filepath.Join(s.Root, "1", strconv.Itoa(sceneID), "cover", key, "manifest.json"))
	if err != nil {
		return 0, err
	}
	var saved struct {
		Version int      `json:"version"`
		Key     string   `json:"key"`
		At      *float64 `json:"at"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return 0, err
	}
	if saved.Version != 1 || saved.Key != key || saved.At == nil || !validTimestamp(*saved.At) {
		return 0, fmt.Errorf("invalid legacy cover source")
	}
	return *saved.At, nil
}

func (s Store) SceneDirectory(sceneID int) string {
	return filepath.Join(s.Root, strconv.Itoa(RecipeVersion), strconv.Itoa(sceneID))
}

func (s Store) directory(sceneID int, kind, revision string) (string, error) {
	if sceneID <= 0 || (kind != "cover" && kind != "marker") || len(revision) != 64 {
		return "", fmt.Errorf("invalid preview image identity")
	}
	for _, c := range revision {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", fmt.Errorf("invalid preview image revision")
		}
	}
	return filepath.Join(s.SceneDirectory(sceneID), kind, revision), nil
}

func (s Store) Load(sceneID int, kind, revision string) (*Manifest, error) {
	dir, err := s.directory(sceneID, kind, revision)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var saved struct {
		Manifest
		At *float64 `json:"at"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, err
	}
	if saved.At == nil || !validTimestamp(*saved.At) {
		return nil, fmt.Errorf("invalid preview image timestamp")
	}
	ret := saved.Manifest
	ret.At = *saved.At
	if ret.Version != RecipeVersion || ret.Key != revision || len(ret.Revision) != 64 || len(ret.Variants) == 0 {
		return nil, fmt.Errorf("invalid preview image manifest")
	}
	for _, variants := range [][]Variant{ret.Variants, ret.Thumbnail} {
		for _, v := range variants {
			if !validVariant(v) {
				return nil, fmt.Errorf("invalid preview image variant")
			}
			if stat, err := os.Stat(filepath.Join(dir, v.File)); err != nil || stat.Size() == 0 {
				return nil, fmt.Errorf("missing preview image variant %s", v.File)
			}
		}
	}
	return &ret, nil
}

func validTimestamp(at float64) bool {
	return at >= 0 && !math.IsNaN(at) && !math.IsInf(at, 0)
}

func validVariant(v Variant) bool {
	if v.Width <= 0 || v.Height <= 0 {
		return false
	}
	if filepath.Base(v.File) != v.File {
		return false
	}
	switch filepath.Ext(v.File) {
	case ".jpg":
		return v.MIMEType == "image/jpeg" && v.DynamicRange == SDR
	case ".avif":
		return v.MIMEType == "image/avif" && (v.DynamicRange == SDR || v.DynamicRange == HDR || v.DynamicRange == Adaptive)
	}
	return false
}

// Publish writes immutable, content-addressed renditions, then atomically
// replaces their manifest. Regeneration can repair missing files or upgrade a
// plain AVIF to a gain map without reusing a cached URL for different pixels.
func (s Store) Publish(sceneID int, kind, revision string, at float64, result *Result) error {
	if !validTimestamp(at) {
		return fmt.Errorf("invalid preview image timestamp")
	}
	dir, err := s.directory(sceneID, kind, revision)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	manifest := Manifest{Version: RecipeVersion, Key: revision, At: at,
		Variants: append([]Variant(nil), result.Variants...), Thumbnail: append([]Variant(nil), result.Thumbnail...)}
	for _, variants := range [][]Variant{manifest.Variants, manifest.Thumbnail} {
		if variants == nil {
			continue
		}
		hasFallback := false
		for i, v := range variants {
			if !validVariant(v) {
				return fmt.Errorf("invalid preview image variant")
			}
			data, err := os.ReadFile(filepath.Join(result.Directory, v.File))
			if err != nil {
				return err
			}
			if len(data) == 0 {
				return fmt.Errorf("empty preview image variant")
			}
			name := fmt.Sprintf("%x%s", sha256.Sum256(data), filepath.Ext(v.File))
			if err := os.Rename(filepath.Join(result.Directory, v.File), filepath.Join(dir, name)); err != nil {
				return err
			}
			variants[i].File = name
			hasFallback = hasFallback || v.MIMEType == "image/jpeg"
		}
		if !hasFallback {
			return fmt.Errorf("preview image requires an SDR fallback")
		}
	}
	if len(manifest.Variants) == 0 {
		return fmt.Errorf("preview image requires full-size renditions")
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	manifest.Revision = fmt.Sprintf("%x", sha256.Sum256(data))
	data, err = json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(result.Directory, "manifest.json"), data, 0644); err != nil {
		return err
	}
	return os.Rename(filepath.Join(result.Directory, "manifest.json"), filepath.Join(dir, "manifest.json"))
}

// File resolves only names present in a validated manifest, never arbitrary
// user paths. The HTTP adapter remains responsible for entity authorization.
func (s Store) File(sceneID int, kind string, manifest *Manifest, name string) (string, *Variant) {
	dir, err := s.directory(sceneID, kind, manifest.Key)
	if err != nil {
		return "", nil
	}
	for _, variants := range [][]Variant{manifest.Variants, manifest.Thumbnail} {
		for _, v := range variants {
			if v.File == name {
				return filepath.Join(dir, name), &v
			}
		}
	}
	return "", nil
}
