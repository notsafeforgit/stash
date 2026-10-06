package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) sourceAlbumMedia(w http.ResponseWriter, r *http.Request) {
	limit, err := documentLimit(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	after := -1
	if r.URL.Query().Has("after") {
		after, err = strconv.Atoi(r.URL.Query().Get("after"))
		if err != nil || after < 0 {
			ingestError(w, ingest.ErrInvalid)
			return
		}
	}
	var result *models.SourceAlbumPage
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceGallery.ReadAlbum(ctx, chi.URLParam(r, "post"), after, limit)
		if err == nil && result == nil {
			return ingest.ErrNotFound
		}
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) sourceGalleryPosts(w http.ResponseWriter, r *http.Request) {
	limit, err := documentLimit(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	var result *models.SourceGalleryPosts
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceGallery.PostsForGallery(ctx, chi.URLParam(r, "entity"), r.URL.Query().Get("after"), limit)
		if err == nil && result == nil {
			return ingest.ErrNotFound
		}
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
