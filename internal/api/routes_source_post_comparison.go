package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) compareSourcePosts(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query) != 1 || len(query["other"]) != 1 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.SourcePostComparison
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceEvidence.ComparePosts(ctx, chi.URLParam(r, "post"), query.Get("other"))
		return err
	})
	switch {
	case errors.Is(err, models.ErrSourcePostComparisonInvalid):
		ingestError(w, ingest.ErrInvalid)
	case errors.Is(err, models.ErrSourcePostComparisonMissing):
		ingestError(w, ingest.ErrNotFound)
	case errors.Is(err, models.ErrSourcePostComparisonLimit):
		ingestJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "post_comparison_limit", "message": "A post exceeds the bounded comparison limit. No choices have been omitted or changed."})
	case err != nil:
		nativeArchiveError(w, err)
	default:
		ingestJSON(w, http.StatusOK, result)
	}
}
