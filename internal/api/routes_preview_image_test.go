package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/previewimage"
)

func TestPreviewImageDelivery(t *testing.T) {
	store := previewimage.Store{Root: t.TempDir()}
	stage, err := os.MkdirTemp(store.Root, ".preview-*")
	if err != nil {
		t.Fatal(err)
	}
	result := &previewimage.Result{Directory: stage, Variants: []previewimage.Variant{
		{File: "preview.jpg", MIMEType: "image/jpeg", DynamicRange: previewimage.SDR, Width: 64, Height: 48},
		{File: "preview.avif", MIMEType: "image/avif", DynamicRange: previewimage.Adaptive, Width: 64, Height: 48},
	}, Thumbnail: []previewimage.Variant{
		{File: "thumbnail.jpg", MIMEType: "image/jpeg", DynamicRange: previewimage.SDR, Width: 32, Height: 24},
		{File: "thumbnail.avif", MIMEType: "image/avif", DynamicRange: previewimage.Adaptive, Width: 32, Height: 24},
	}}
	defer result.Close()
	for _, v := range append(result.Variants, result.Thumbnail...) {
		if err := os.WriteFile(filepath.Join(stage, v.File), []byte(v.File), 0600); err != nil {
			t.Fatal(err)
		}
	}
	key := strings.Repeat("a", 64)
	if err := store.Publish(12, "cover", key, 1, result); err != nil {
		t.Fatal(err)
	}
	manifest, err := store.Load(12, "cover", key)
	if err != nil {
		t.Fatal(err)
	}
	image := previewImageModel("https://example.test/stash/scene/12/preview-image", manifest)
	if image == nil || len(image.Sources) != 1 || image.Sources[0].DynamicRange != PreviewImageDynamicRangeAdaptive || !strings.HasPrefix(image.Fallback, "https://example.test/stash/") {
		t.Fatalf("incorrect image catalog: %+v", image)
	}
	if image.Thumbnail == nil || len(image.Thumbnail.Sources) != 1 || image.Thumbnail.Sources[0].Width != 32 || image.Thumbnail.Fallback == image.Fallback {
		t.Fatalf("missing independent thumbnail catalog: %+v", image.Thumbnail)
	}
	for _, test := range []struct {
		name     string
		revision string
		file     string
		manifest *previewimage.Manifest
		want     int
	}{
		{"avif", manifest.Revision, manifest.Variants[1].File, manifest, http.StatusOK},
		{"thumbnail", manifest.Revision, manifest.Thumbnail[1].File, manifest, http.StatusOK},
		{"stale revision", "old", manifest.Variants[1].File, manifest, http.StatusNotFound},
		{"obsolete cover", manifest.Revision, manifest.Variants[1].File, nil, http.StatusNotFound},
		{"traversal", manifest.Revision, "../manifest.json", manifest, http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/preview?revision="+test.revision, nil)
			rec := httptest.NewRecorder()
			servePreviewImage(rec, req, store, 12, "cover", test.manifest, test.file)
			if rec.Code != test.want {
				t.Fatalf("status = %d, want %d", rec.Code, test.want)
			}
			wantBody := "preview.avif"
			if test.name == "thumbnail" {
				wantBody = "thumbnail.avif"
			}
			if test.want == http.StatusOK && (rec.Header().Get("Content-Type") != "image/avif" || rec.Body.String() != wantBody || !strings.Contains(rec.Header().Get("Cache-Control"), "private")) {
				t.Fatalf("incorrect AVIF delivery: %v %s", rec.Header(), rec.Body.String())
			}
			if test.want == http.StatusOK {
				if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Content-Security-Policy") != "default-src 'none'; img-src data:; style-src 'unsafe-inline'; sandbox" {
					t.Fatalf("missing preview image security headers: %v", rec.Header())
				}
			}
		})
	}
	// Old manifests remain usable without a thumbnail or new URL format.
	manifest.Thumbnail = nil
	if old := previewImageModel("https://example.test/preview", manifest); old == nil || old.Thumbnail != nil {
		t.Fatalf("older cover is not backwards compatible: %+v", old)
	}
}
