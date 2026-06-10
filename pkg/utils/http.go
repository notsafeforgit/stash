package utils

import (
	"bytes"
	"errors"
	"io/fs"
	"net/http"
	"path/filepath"
	"time"

	"github.com/stashapp/stash/pkg/hash/md5"
)

// Returns an MD5 hash of data, formatted for use as an HTTP ETag header.
// Intended for use with `http.ServeContent`, to respond to conditional requests.
func GenerateETag(data []byte) string {
	hash := md5.FromBytes(data)
	return `"` + hash + `"`
}

func setStaticContentCacheControl(w http.ResponseWriter, r *http.Request) {
	// Honour an explicit Cache-Control set by the caller. Used by routes
	// that conditionally serve placeholder content (e.g. the default
	// gallery / image SVG when no real cover is available yet) which
	// must not be cached as immutable just because the URL happens to
	// carry a `?t=` token — the token reflects the parent's UpdatedAt,
	// not the placeholder fallback's lifetime.
	if w.Header().Get("Cache-Control") != "" {
		return
	}
	if r.URL.Query().Has("t") {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
}

// Serves static content, adding Cache-Control: no-cache and a generated ETag header.
// Responds to conditional requests using the ETag.
func ServeStaticContent(w http.ResponseWriter, r *http.Request, data []byte) {
	setStaticContentCacheControl(w, r)
	w.Header().Set("ETag", GenerateETag(data))

	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// Serves static content at filepath, adding Cache-Control: no-cache.
// Responds to conditional requests using the file modtime.
func ServeStaticFile(w http.ResponseWriter, r *http.Request, filepath string) {
	setStaticContentCacheControl(w, r)

	http.ServeFile(w, r, filepath)
}

func toHTTPError(err error) (msg string, httpStatus int) {
	if errors.Is(err, fs.ErrNotExist) {
		return "404 page not found", http.StatusNotFound
	}
	if errors.Is(err, fs.ErrPermission) {
		return "403 Forbidden", http.StatusForbidden
	}
	return "500 Internal Server Error", http.StatusInternalServerError
}

// ServeStaticFileModTime serves a static file at the given path using the given modTime instead of the file modTime.
func ServeStaticFileModTime(w http.ResponseWriter, r *http.Request, path string, modTime time.Time) {
	setStaticContentCacheControl(w, r)

	dir, file := filepath.Split(path)
	fs := http.Dir(dir)

	f, err := fs.Open(file)
	if err != nil {
		msg, code := toHTTPError(err)
		http.Error(w, msg, code)
		return
	}
	defer f.Close()

	d, err := f.Stat()
	if err != nil {
		msg, code := toHTTPError(err)
		http.Error(w, msg, code)
		return
	}

	http.ServeContent(w, r, d.Name(), modTime, f)
}
