package ui

import (
	"bytes"
	"io/fs"
	"testing"
)

func TestUIBoxIncludesUnderscoreRouteChunks(t *testing.T) {
	matches, err := fs.Glob(UIBox, "assets/_*.js.gz")
	if err != nil {
		t.Fatalf("glob embedded v3 route chunks: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected embedded v3 UI to include underscore-prefixed route chunks")
	}
}

func TestNativeEmbeddedEntryPoints(t *testing.T) {
	// All entry points must come from the same build, including when a checkout
	// has never installed or built the retired application.
	for _, name := range []string{"index.html", "share.html", "offline.html", "service-worker.js", "manifest.json", "favicon.ico", "favicon.png"} {
		t.Run(name, func(t *testing.T) {
			data, err := fs.ReadFile(UIBox, name)
			if err != nil {
				t.Fatal(err)
			}
			if len(bytes.TrimSpace(data)) == 0 {
				t.Fatal("empty embedded asset; build the native UI before testing")
			}
		})
	}
	if len(FaviconProvider.GetFavicon()) == 0 || len(FaviconProvider.GetFaviconPng()) == 0 {
		t.Fatal("native tray icons are missing")
	}
}
