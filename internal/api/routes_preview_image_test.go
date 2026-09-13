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
	}}
	defer result.Close()
	for _, v := range result.Variants {
		if err := os.WriteFile(filepath.Join(stage, v.File), []byte(v.MIMEType), 0600); err != nil {
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
	for _, test := range []struct {
		name     string
		revision string
		file     string
		manifest *previewimage.Manifest
		want     int
	}{
		{"avif", manifest.Revision, manifest.Variants[1].File, manifest, http.StatusOK},
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
			if test.want == http.StatusOK && (rec.Header().Get("Content-Type") != "image/avif" || rec.Body.String() != "image/avif" || !strings.Contains(rec.Header().Get("Cache-Control"), "private")) {
				t.Fatalf("incorrect AVIF delivery: %v %s", rec.Header(), rec.Body.String())
			}
		})
	}
}
