package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) browseSourcePosts(w http.ResponseWriter, r *http.Request) {
	limit, err := documentLimit(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	query := r.URL.Query()
	filter := models.SourcePostFilter{After: query.Get("after"), Limit: limit, PostUUID: query.Get("uuid"), URL: query.Get("url")}
	if query.Has("namespace") || query.Has("value") {
		filter.Identifier = &models.SourcePostIdentifier{Namespace: query.Get("namespace"), Value: query.Get("value")}
	}
	var result []models.SourcePostSummary
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceEvidence.BrowsePosts(ctx, filter)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) sourcePost(w http.ResponseWriter, r *http.Request) {
	var result *models.SourcePostSummary
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceEvidence.PostSummary(ctx, chi.URLParam(r, "post"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

// Edits use the current canonical revision. Original capture/history endpoints
// keep their existing scopes, and a saved request is never rewritten on redirect.
func (rs *nativeArchiveRoutes) sourcePostIdentity(w http.ResponseWriter, r *http.Request) {
	if !ingest.ValidUUID(chi.URLParam(r, "post")) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var requested, canonical *models.SourcePostIdentity
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		requested, err = rs.repo.SourceEvidence.PostIdentity(ctx, chi.URLParam(r, "post"))
		if err != nil {
			return err
		}
		if requested == nil {
			return ingest.ErrNotFound
		}
		canonical = requested
		if requested.CanonicalUUID != requested.UUID {
			canonical, err = rs.repo.SourceEvidence.PostIdentity(ctx, requested.CanonicalUUID)
			if err != nil {
				return err
			}
		}
		if canonical == nil || canonical.UUID != canonical.CanonicalUUID || canonical.RedirectTo != nil {
			return models.ErrSourcePayloadCorrupt
		}
		return nil
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]any{"requested": requested, "canonical": canonical})
}

func (rs *nativeArchiveRoutes) sourcePostIdentifiers(w http.ResponseWriter, r *http.Request) {
	post, query := chi.URLParam(r, "post"), r.URL.Query()
	limit, err := documentLimit(r)
	if err != nil || !ingest.ValidUUID(post) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var after *models.SourcePostIdentifier
	if query.Has("after_namespace") || query.Has("after_value") {
		after = &models.SourcePostIdentifier{Namespace: query.Get("after_namespace"), Value: query.Get("after_value")}
		if after.Namespace == "" || after.Value == "" {
			ingestError(w, ingest.ErrInvalid)
			return
		}
	}
	result := []models.SourcePostIdentifierSummary{}
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		rows, err := rs.repo.SourceEvidence.PostIdentifiers(ctx, post, after, limit)
		if err != nil {
			return err
		}
		for _, row := range rows {
			result = append(result, models.SourcePostIdentifierSummary(row))
		}
		return nil
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

// Only selected capture publishers appear here. Retained account claims and
// account owners are different concepts; neither assigns depicted performers.
func (rs *nativeArchiveRoutes) sourcePostPublishers(w http.ResponseWriter, r *http.Request) {
	post, after := chi.URLParam(r, "post"), r.URL.Query().Get("after")
	limit, err := documentLimit(r)
	if err != nil || !ingest.ValidUUID(post) || (after != "" && !ingest.ValidUUID(after)) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	result := []models.AccountReviewState{}
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		accounts, err := rs.repo.CapturePublisher.PostAccounts(ctx, post, after, limit)
		if err != nil {
			return err
		}
		for _, account := range accounts {
			review, err := rs.repo.SourceAccount.ReviewAccount(ctx, account.UUID)
			if err != nil {
				return err
			}
			if review == nil {
				return models.ErrSourceAccountConflict
			}
			result = append(result, *review)
		}
		return nil
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) sourcePostAlbum(w http.ResponseWriter, r *http.Request) {
	post := chi.URLParam(r, "post")
	if !ingest.ValidUUID(post) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.SourcePostAlbum
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		found, err := rs.repo.SourceEvidence.FindPost(ctx, post)
		if err != nil {
			return err
		}
		if found == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.SourceGallery.AssociationView(ctx, post)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]any{"requested_uuid": post, "album": result})
}

func (rs *nativeArchiveRoutes) sourcePostMedia(w http.ResponseWriter, r *http.Request) {
	limit, err := documentLimit(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	var result []models.SourcePostMediaItem
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourcePostMedia.MediaForPost(ctx, chi.URLParam(r, "post"), r.URL.Query().Get("after"), limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
