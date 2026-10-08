package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) entityProviderMetadataHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "entity")
	if !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	after, limit := 0, 50
	for key, dest := range map[string]*int{"after": &after, "limit": &limit} {
		if value := r.URL.Query().Get(key); value != "" {
			var err error
			*dest, err = strconv.Atoi(value)
			if err != nil || *dest < 0 {
				ingestError(w, ingest.ErrInvalid)
				return
			}
		}
	}
	if limit < 1 || limit > 100 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []models.ProviderMetadataImport
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		entity, err := rs.repo.ArchiveEntity.Find(ctx, id)
		if err != nil {
			return err
		}
		if entity == nil {
			return ingest.ErrNotFound
		}
		if len(models.ProviderMetadataFields(entity.Kind)) == 0 {
			return ingest.ErrInvalid
		}
		result, err = rs.repo.ProviderMetadata.History(ctx, id, after, limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
