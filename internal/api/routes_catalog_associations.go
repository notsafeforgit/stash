package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) previewCatalogAssociation(w http.ResponseWriter, r *http.Request) {
	if !ingest.ValidUUID(chi.URLParam(r, "post")) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.CatalogAssociationPreview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CapturePublisher.PreviewCatalogAssociation(ctx, chi.URLParam(r, "post"))
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) catalogAssociationPosts(w http.ResponseWriter, r *http.Request) {
	after, limit, err := accountReviewPage(r)
	if err != nil || (after != "" && !ingest.ValidUUID(after)) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []string
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CapturePublisher.CatalogAssociationPosts(ctx, after, limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

// This explicit admin repair is separate from the strict author-ID policy for
// new ingestion. Each small batch rereads all evidence inside its transaction.
// Repeating a batch preserves current choices, including intervening unlinks.
func (rs *nativeArchiveRoutes) backfillCatalogAssociations(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PostUUIDs  []string `json:"post_uuids"`
		LinkOwners bool     `json:"link_owners"`
	}
	if err := readIngestJSON(w, r, 8192, &input); err != nil {
		ingestError(w, err)
		return
	}
	if len(input.PostUUIDs) < 1 || len(input.PostUUIDs) > 25 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	seen := map[string]bool{}
	for _, id := range input.PostUUIDs {
		if !ingest.ValidUUID(id) || seen[id] {
			ingestError(w, ingest.ErrInvalid)
			return
		}
		seen[id] = true
	}
	results := make([]models.CatalogAssociationResult, 0, len(input.PostUUIDs))
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		for _, id := range input.PostUUIDs {
			result, err := rs.repo.CapturePublisher.BackfillCatalogAssociation(ctx, id, input.LinkOwners)
			if err != nil {
				return err
			}
			results = append(results, *result)
		}
		return nil
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, results)
}
