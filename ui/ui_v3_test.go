package ui

import (
	"io/fs"
	"testing"
)

func TestUIV3BoxIncludesUnderscoreRouteChunks(t *testing.T) {
	matches, err := fs.Glob(UIV3Box, "assets/_*.js.gz")
	if err != nil {
		t.Fatalf("glob embedded v3 route chunks: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected embedded v3 UI to include underscore-prefixed route chunks")
	}
}
