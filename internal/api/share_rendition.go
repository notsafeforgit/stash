package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/stashapp/stash/internal/sharing"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/ffmpeg/transcoder"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *shareRoutes) rendition(w http.ResponseWriter, r *http.Request) {
	item, f, _, err := rs.item(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	size := 640
	if strings.HasSuffix(r.URL.Path, "/image") {
		size = 4096
	}
	identity, err := json.Marshal(struct {
		File        models.FileID
		Fingerprint models.SceneCoverFingerprint
		Size        int
	}{item.FileID, item.Fingerprint, size})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	key := hex.EncodeToString(sharing.Hash(string(identity)))
	dir := filepath.Join(rs.server.manager.Config.GetGeneratedPath(), "shares")
	cache := filepath.Join(dir, key+".jpg")
	w.Header().Set("Content-Type", "image/jpeg")
	if data, err := os.ReadFile(cache); err == nil {
		http.ServeContent(w, r, "image.jpg", time.Time{}, bytes.NewReader(data))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	select {
	case rs.budget.renders <- struct{}{}:
		defer func() { <-rs.budget.renders }()
	case <-ctx.Done():
		http.Error(w, "Share is busy", http.StatusTooManyRequests)
		return
	}
	// A parallel request may have filled the cache while this request waited.
	if data, err := os.ReadFile(cache); err == nil {
		http.ServeContent(w, r, "image.jpg", time.Time{}, bytes.NewReader(data))
		return
	}
	input := f.Base().Path
	if f.Base().ZipFileID != nil {
		// Seekable input also supports AVIF/HEIF archive members. Names are
		// generated locally and never appear in a guest response or URL.
		reader, err := f.Open(&file.OsFS{})
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer reader.Close()
		temp, err := os.CreateTemp("", "stash-share-image-*")
		if err != nil {
			http.Error(w, "Media unavailable", http.StatusServiceUnavailable)
			return
		}
		defer os.Remove(temp.Name())
		const limit = 256 * 1024 * 1024
		n, err := io.Copy(temp, io.LimitReader(reader, limit+1))
		closeErr := temp.Close()
		if err != nil || closeErr != nil || n > limit {
			http.Error(w, "Media unavailable", http.StatusServiceUnavailable)
			return
		}
		input = temp.Name()
	}
	args := transcoder.ImageThumbnail(input, transcoder.ImageThumbnailOptions{OutputFormat: ffmpeg.ImageFormatJpeg, OutputPath: "-", MaxDimensions: size, Quality: 2})
	args = slices.Insert(args, slices.Index(args, "-i")+2, "-map_metadata", "-1", "-map_metadata:s", "-1", "-map_chapters", "-1")
	// This always decodes and re-encodes. Never fall back to original files:
	// JPEG output contains no EXIF/GPS or arbitrary embedded source metadata.
	data, err := rs.server.manager.FFMpeg.GenerateOutput(ctx, args, nil)
	if err != nil || len(data) == 0 {
		http.Error(w, "Media unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := os.MkdirAll(dir, 0o700); err == nil {
		if temp, err := os.CreateTemp(dir, ".rendition-*"); err == nil {
			name := temp.Name()
			defer os.Remove(name)
			_, writeErr := temp.Write(data)
			closeErr := temp.Close()
			if writeErr == nil && closeErr == nil {
				_ = os.Rename(name, cache)
			}
		}
	}
	http.ServeContent(w, r, "image.jpg", time.Time{}, bytes.NewReader(data))
}
