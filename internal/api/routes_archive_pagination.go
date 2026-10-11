package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/stashapp/stash/pkg/models"
)

// Counts have their own read so large filtered counts never hold up the first
// page. They use application authentication, just like the corresponding lists.
func (rs *nativeArchiveRoutes) archivePageInfo(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	allowed := map[string]bool{"resource": true, "subject": true, "cursor": true, "q": true, "state": true, "kind": true, "namespace": true, "ownership": true, "scope": true, "uuid": true, "url": true, "value": true, "collection": true}
	for key, values := range q {
		if !allowed[key] || len(values) != 1 || len(values[0]) > 8192 {
			ingestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_pagination_request"})
			return
		}
	}
	filter := models.ArchivePageFilter{Resource: q.Get("resource"), Subject: q.Get("subject"), Cursor: q.Get("cursor"), Query: q.Get("q"), State: q.Get("state"), Kind: q.Get("kind"), Namespace: q.Get("namespace"), Ownership: q.Get("ownership"), Scope: q.Get("scope"), PostUUID: q.Get("uuid"), URL: q.Get("url"), Value: q.Get("value"), Collection: q.Get("collection")}
	var info *models.ArchivePageInfo
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		info, err = rs.repo.ArchivePagination.PageInfo(ctx, filter)
		return err
	})
	if errors.Is(err, models.ErrArchivePageInvalid) {
		ingestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_pagination_request"})
		return
	}
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, info)
}
