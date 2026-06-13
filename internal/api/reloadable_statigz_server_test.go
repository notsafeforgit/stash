package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"
)

func TestReloadableStatigzServerRefreshesWhenIndexChanges(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": {
			Data:    []byte(`<script type="module" src="/assets/old.js"></script>`),
			Mode:    0o644,
			ModTime: time.Unix(1, 0),
		},
		"assets/old.js": {
			Data: []byte("old"),
			Mode: 0o644,
		},
	}

	server := newReloadableStatigzServer(fsys)

	requestAsset(t, server, "/assets/old.js", http.StatusOK)

	delete(fsys, "assets/old.js")
	fsys["index.html"] = &fstest.MapFile{
		Data:    []byte(`<script type="module" src="/assets/new.js"></script>`),
		Mode:    0o644,
		ModTime: time.Unix(2, 0),
	}
	fsys["assets/new.js"] = &fstest.MapFile{
		Data: []byte("new"),
		Mode: 0o644,
	}

	recorder := requestAsset(t, server, "/assets/new.js", http.StatusOK)
	if got := recorder.Body.String(); got != "new" {
		t.Fatalf("expected refreshed asset body %q, got %q", "new", got)
	}
}

func TestReloadableStatigzServerRetriesMissingAsset(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": {
			Data:    []byte(`<script type="module" src="/assets/later.js"></script>`),
			Mode:    0o644,
			ModTime: time.Unix(1, 0),
		},
	}

	server := newReloadableStatigzServer(fsys)

	requestAsset(t, server, "/assets/later.js", http.StatusNotFound)

	fsys["assets/later.js"] = &fstest.MapFile{
		Data: []byte("later"),
		Mode: 0o644,
	}

	recorder := requestAsset(t, server, "/assets/later.js", http.StatusOK)
	if got := recorder.Body.String(); got != "later" {
		t.Fatalf("expected refreshed asset body %q, got %q", "later", got)
	}
}

func requestAsset(t *testing.T, server http.Handler, path string, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)

	if recorder.Code != wantStatus {
		t.Fatalf("expected status %d for %s, got %d", wantStatus, path, recorder.Code)
	}

	return recorder
}
