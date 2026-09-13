package previewimage

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreIdentityAndPublication(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	revision, err := SourceKey(source, "cover-a")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := SourceKey(source, "cover-b")
	if revision == other {
		t.Fatal("a custom cover must invalidate generated artwork")
	}
	marker1, _ := SourceKey(source, "1.1")
	marker2, _ := SourceKey(source, "1.9")
	if marker1 == marker2 {
		t.Fatal("fractional markers must not collide")
	}
	store := Store{Root: root}
	if _, err := store.Load(1, "cover", revision); !os.IsNotExist(err) {
		t.Fatalf("unpublished entry: %v", err)
	}
	for range 2 {
		dir, err := os.MkdirTemp(root, ".preview-*")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "preview.jpg"), []byte("jpeg"), 0600); err != nil {
			t.Fatal(err)
		}
		result := &Result{Directory: dir, Variants: []Variant{{"preview.jpg", "image/jpeg", SDR, 64, 48}}}
		defer result.Close()
		if err := store.Publish(1, "cover", revision, 1.5, result); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := store.Load(1, "cover", revision)
	if err != nil {
		t.Fatal(err)
	}
	path, variant := store.File(1, "cover", manifest, manifest.Variants[0].File)
	if variant == nil || path == "" || manifest.At != 1.5 {
		t.Fatal("published entry is incomplete")
	}
	if path, _ := store.File(1, "cover", manifest, "../../source"); path != "" {
		t.Fatal("allowed traversal")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(1, "cover", revision); err == nil {
		t.Fatal("advertised a missing rendition")
	}
	// Regeneration repairs a deleted rendition and publishes a new immutable
	// URL when pixels or encoding capabilities change.
	dir, err := os.MkdirTemp(root, ".preview-*")
	if err != nil {
		t.Fatal(err)
	}
	result := &Result{Directory: dir, Variants: []Variant{{"preview.jpg", "image/jpeg", SDR, 64, 48}}}
	defer result.Close()
	if err := os.WriteFile(filepath.Join(dir, "preview.jpg"), []byte("new jpeg"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(1, "cover", revision, 1.5, result); err != nil {
		t.Fatal(err)
	}
	repaired, err := store.Load(1, "cover", revision)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Revision == repaired.Revision || manifest.Variants[0].File == repaired.Variants[0].File {
		t.Fatal("regeneration reused a URL for different pixels")
	}
	if err := os.WriteFile(source, []byte("replacement video"), 0600); err != nil {
		t.Fatal(err)
	}
	replaced, _ := SourceKey(source, "cover-a")
	if replaced == revision {
		t.Fatal("replacement source reused obsolete artwork")
	}
}

func TestStoreTimestampValidation(t *testing.T) {
	store := Store{Root: t.TempDir()}
	key := strings.Repeat("a", 64)
	dir, err := store.directory(1, "cover", key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "preview.jpg"), []byte("jpeg"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		at   any
		want *float64
	}{
		{name: "zero", at: 0.0, want: floatPointer(0)},
		{name: "fractional", at: 123.456789, want: floatPointer(123.456789)},
		{name: "missing"},
		{name: "null", at: nil},
		{name: "negative", at: -1},
		{name: "string", at: "12.5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string]any{
				"version": RecipeVersion, "key": key, "revision": key,
				"variants": []Variant{{"preview.jpg", "image/jpeg", SDR, 64, 48}},
			}
			if tc.name != "missing" {
				data["at"] = tc.at
			}
			encoded, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "manifest.json"), encoded, 0600); err != nil {
				t.Fatal(err)
			}
			manifest, err := store.Load(1, "cover", key)
			if tc.want == nil {
				if err == nil {
					t.Fatalf("accepted unknown or invalid timestamp: %+v", manifest)
				}
			} else if err != nil || manifest.At != *tc.want {
				t.Fatalf("saved timestamp changed: %+v, %v", manifest, err)
			}
		})
	}
	for _, at := range []float64{-1, math.NaN(), math.Inf(1)} {
		if err := store.Publish(1, "cover", key, at, nil); err == nil {
			t.Fatalf("published invalid timestamp: %v", at)
		}
	}
}

func floatPointer(value float64) *float64 { return &value }
