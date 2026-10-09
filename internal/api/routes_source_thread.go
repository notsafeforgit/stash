package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) sourcePostThread(w http.ResponseWriter, r *http.Request) {
	limit, err := documentLimit(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	var result *models.SourceThreadView
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceThread.Read(ctx, chi.URLParam(r, "post"), r.URL.Query().Get("after"), limit)
		if err == nil && result == nil {
			return ingest.ErrNotFound
		}
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]any{"requested_uuid": chi.URLParam(r, "post"), "thread": result})
}
