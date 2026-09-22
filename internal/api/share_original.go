package api

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *shareRoutes) serveOriginalMedia() bool {
	return config.GetInstance().GetSharingServeOriginalMedia()
}

func (rs *shareRoutes) originalStream(w http.ResponseWriter, r *http.Request) {
	if !rs.serveOriginalMedia() {
		http.NotFound(w, r)
		return
	}
	item, f, scene, err := rs.item(r)
	if err != nil || scene == nil {
		http.NotFound(w, r)
		return
	}
	rs.serveOriginal(w, r, item, f, false)
}

func sharedOriginalMIME(f models.File) string {
	t := mime.TypeByExtension(strings.ToLower(path.Ext(f.Base().Basename)))
	switch f.(type) {
	case *models.ImageFile:
		if strings.HasPrefix(t, "image/") {
			return t
		}
	case *models.VideoFile:
		if strings.HasPrefix(t, "video/") {
			return t
		}
	}
	return "application/octet-stream"
}

// The caller must resolve the pinned item first. Regular files are streamed
// directly; archive members are decompressed without buffering a whole file or
// exposing the containing archive. No media encoder or generated cache is used.
func (rs *shareRoutes) serveOriginal(w http.ResponseWriter, r *http.Request, item *models.ShareMedia, f models.File, download bool) {
	if f.Base().ZipFileID != nil {
		// Each backward range would decompress the member again. Browsers seek
		// with a single range; reject multipart ranges to bound amplification.
		if strings.Contains(r.Header.Get("Range"), ",") {
			http.Error(w, "Range unavailable", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		select {
		case rs.budget.archives <- struct{}{}:
			defer func() { <-rs.budget.archives }()
		default:
			w.Header().Set("Retry-After", "5")
			http.Error(w, "Share is busy", http.StatusTooManyRequests)
			return
		}
	}
	reader, err := f.Open(&file.OsFS{})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var content io.ReadSeeker
	if seekable, ok := reader.(io.ReadSeeker); ok {
		defer reader.Close()
		content = seekable
	} else {
		archive := &sharedArchiveReader{ctx: r.Context(), file: f, reader: reader, size: f.Base().Size}
		defer archive.close()
		content = archive
	}
	ext := strings.ToLower(path.Ext(f.Base().Basename))
	if len(ext) > 12 || strings.ContainsAny(ext, "\"\\/\r\n") {
		ext = ""
	}
	name := item.Key + ext
	disposition := "inline"
	mediaType := sharedOriginalMIME(f)
	if download || mediaType == "application/octet-stream" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	// Originals can include SVG or other active content. Opening one as a
	// document must not run scripts, load external resources or access cookies.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'; sandbox; base-uri 'none'; frame-ancestors 'none'")
	http.ServeContent(w, r, name, time.Time{}, content)
}

// Zip members are not seekable. Defer seeking until Read so HEAD and the size
// probe in ServeContent don't decompress anything. Backward seeks
// reopen only this member; skipping and delivery use bounded memory and honor
// the grant's cancellation context.
type sharedArchiveReader struct {
	ctx      context.Context
	file     models.File
	reader   io.ReadCloser
	size     int64
	position int64
	readAt   int64
}

func (s *sharedArchiveReader) close() {
	if s.reader != nil {
		_ = s.reader.Close()
	}
}

func (s *sharedArchiveReader) Seek(offset int64, whence int) (int64, error) {
	base := int64(0)
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = s.position
	case io.SeekEnd:
		base = s.size
	default:
		return 0, errors.New("invalid seek")
	}
	position := base + offset
	if position < 0 || (offset > 0 && position < base) {
		return 0, errors.New("invalid seek")
	}
	s.position = position
	return position, nil
}

func (s *sharedArchiveReader) Read(p []byte) (int, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	if s.position >= s.size {
		return 0, io.EOF
	}
	if s.position < s.readAt {
		s.close()
		reader, err := s.file.Open(&file.OsFS{})
		s.reader = reader
		if err != nil {
			return 0, err
		}
		s.readAt = 0
	}
	var buffer [32 * 1024]byte
	for s.readAt < s.position {
		if err := s.ctx.Err(); err != nil {
			return 0, err
		}
		n, err := s.reader.Read(buffer[:min(int64(len(buffer)), s.position-s.readAt)])
		s.readAt += int64(n)
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, io.ErrNoProgress
		}
	}
	n, err := s.reader.Read(p[:min(int64(len(p)), s.size-s.position)])
	s.readAt += int64(n)
	s.position += int64(n)
	return n, err
}
