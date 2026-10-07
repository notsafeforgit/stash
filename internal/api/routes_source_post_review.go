package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) mediaSourcePosts(w http.ResponseWriter, r *http.Request) {
	limit, err := documentLimit(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	var ret []models.SourcePostMediaReview
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.SourcePostMedia.PostsForMedia(ctx, chi.URLParam(r, "entity"), r.URL.Query().Get("after"), limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, ret)
}

func (rs *nativeArchiveRoutes) postMediaReview(w http.ResponseWriter, r *http.Request) {
	var ret *models.SourcePostMediaReview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.SourcePostMedia.Review(ctx, chi.URLParam(r, "post"), chi.URLParam(r, "entity"))
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, ret)
}

func (rs *nativeArchiveRoutes) reviewPostURLs(w http.ResponseWriter, r *http.Request) {
	post, after := chi.URLParam(r, "post"), r.URL.Query().Get("after")
	limit, err := documentLimit(r)
	if err != nil || !ingest.ValidUUID(post) || (after != "" && !ingest.ValidUUID(after)) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var ret []models.SourcePostURL
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.SourcePostLinks.CurrentURLs(ctx, post, after, limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]any{"requested_uuid": post, "urls": ret})
}

type sourceReviewCapture struct {
	UUID             string     `json:"uuid"`
	PostUUID         string     `json:"post_uuid"`
	RevisionUUID     string     `json:"revision_uuid"`
	Origin           string     `json:"origin"`
	Platform         string     `json:"platform"`
	CapturedAt       *time.Time `json:"captured_at"`
	RecordedAt       *time.Time `json:"recorded_at"`
	ExtractorVersion *string    `json:"extractor_version"`
}

type sourceReviewRevision struct {
	UUID     string                    `json:"uuid"`
	Metadata models.SourcePostMetadata `json:"metadata"`
}

func (rs *nativeArchiveRoutes) reviewPostCaptures(w http.ResponseWriter, r *http.Request) {
	post := chi.URLParam(r, "post")
	limit, err := documentLimit(r)
	if err != nil || !ingest.ValidUUID(post) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var after *models.SourceCaptureCursor
	query := r.URL.Query()
	if query.Has("after_uuid") || query.Has("after_time") || query.Has("after_clock") {
		clock, parseErr := time.Parse(time.RFC3339Nano, query.Get("after_time"))
		kind := query.Get("after_clock")
		if parseErr != nil || clock.IsZero() || !ingest.ValidUUID(query.Get("after_uuid")) || (kind != "observed" && kind != "recorded") {
			ingestError(w, ingest.ErrInvalid)
			return
		}
		after = &models.SourceCaptureCursor{UUID: query.Get("after_uuid")}
		if kind == "observed" {
			after.CapturedAt = clock
		} else {
			after.RecordedAt = &clock
		}
	}
	ret := struct {
		RequestedUUID string                 `json:"requested_uuid"`
		Captures      []sourceReviewCapture  `json:"captures"`
		Revisions     []sourceReviewRevision `json:"revisions"`
	}{RequestedUUID: post, Captures: []sourceReviewCapture{}, Revisions: []sourceReviewRevision{}}
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		captures, err := rs.repo.SourceEvidence.CurrentCaptures(ctx, post, after, limit)
		if err != nil {
			return err
		}
		seen := make(map[string]bool)
		for _, capture := range captures {
			var observed *time.Time
			if !capture.CapturedAt.IsZero() {
				observed = &capture.CapturedAt
			}
			ret.Captures = append(ret.Captures, sourceReviewCapture{UUID: capture.UUID, PostUUID: capture.PostUUID, RevisionUUID: capture.RevisionUUID,
				Origin: capture.Origin, Platform: capture.Platform, CapturedAt: observed, RecordedAt: capture.RecordedAt, ExtractorVersion: capture.ExtractorVersion})
			if !seen[capture.RevisionUUID] {
				seen[capture.RevisionUUID] = true
				ret.Revisions = append(ret.Revisions, sourceReviewRevision{UUID: capture.RevisionUUID, Metadata: capture.Metadata})
			}
		}
		return nil
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, ret)
}
