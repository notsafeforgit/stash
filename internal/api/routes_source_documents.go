package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func documentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrSourceDocumentInvalid):
		ingestError(w, ingest.ErrInvalid)
	case errors.Is(err, models.ErrSourceDocumentConflict), errors.Is(err, models.ErrSourceDocumentReplay):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "document_changed", "message": "The document selection changed; load its current revision."})
	default:
		nativeArchiveError(w, err)
	}
}

func (rs *nativeArchiveRoutes) readDocument(w http.ResponseWriter, r *http.Request, read func(context.Context) (any, error)) {
	var result any
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = read(ctx)
		return err
	})
	if err != nil {
		documentError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) document(w http.ResponseWriter, r *http.Request) {
	rs.readDocument(w, r, func(ctx context.Context) (any, error) {
		doc, err := rs.repo.SourceDocument.Find(ctx, chi.URLParam(r, "document"))
		if err == nil && doc == nil {
			err = ingest.ErrNotFound
		}
		return doc, err
	})
}

func (rs *nativeArchiveRoutes) documentContent(w http.ResponseWriter, r *http.Request) {
	var doc *models.SourceDocument
	var body []byte
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		doc, err = rs.repo.SourceDocument.Find(ctx, chi.URLParam(r, "document"))
		if err != nil {
			return err
		}
		if doc == nil {
			return ingest.ErrNotFound
		}
		body, err = rs.repo.SourceDocument.Content(ctx, doc.ContentSHA256)
		if err == nil && body == nil {
			return models.ErrSourcePayloadCorrupt
		}
		return err
	})
	if err != nil {
		documentError(w, err)
		return
	}
	// A historical pathname is evidence, never a server file to open. Return
	// retained bytes as a download even when they contain HTML or invalid XML.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="source-document-`+doc.UUID+`.bin"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (rs *nativeArchiveRoutes) documentSource(w http.ResponseWriter, r *http.Request) {
	rs.readDocument(w, r, func(ctx context.Context) (any, error) {
		source, err := rs.repo.SourceDocument.Source(ctx, chi.URLParam(r, "source"))
		if err == nil && source == nil {
			err = ingest.ErrNotFound
		}
		return source, err
	})
}

func (rs *nativeArchiveRoutes) documentClaim(w http.ResponseWriter, r *http.Request) {
	rs.readDocument(w, r, func(ctx context.Context) (any, error) {
		claim, err := rs.repo.SourceDocument.HeadClaim(ctx, chi.URLParam(r, "claim"))
		if err == nil && claim == nil {
			err = ingest.ErrNotFound
		}
		return claim, err
	})
}

func documentLimit(r *http.Request) (int, error) {
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil {
			return 0, ingest.ErrInvalid
		}
	}
	if limit < 1 || limit > 100 {
		return 0, ingest.ErrInvalid
	}
	return limit, nil
}

func (rs *nativeArchiveRoutes) documentLocation(ctx context.Context, collection, path string) error {
	if !ingest.ValidUUID(collection) || !archive.ValidDocumentPath(path) {
		return ingest.ErrInvalid
	}
	value, err := rs.repo.SourceCollection.Find(ctx, collection)
	if err == nil && value == nil {
		return ingest.ErrNotFound
	}
	return err
}

func (rs *nativeArchiveRoutes) postDocuments(w http.ResponseWriter, r *http.Request) {
	limit, err := documentLimit(r)
	if err != nil {
		documentError(w, err)
		return
	}
	rs.readDocument(w, r, func(ctx context.Context) (any, error) {
		id := chi.URLParam(r, "post")
		if !ingest.ValidUUID(id) {
			return nil, ingest.ErrInvalid
		}
		post, err := rs.repo.SourceEvidence.FindPost(ctx, id)
		if err != nil {
			return nil, err
		}
		if post == nil {
			return nil, ingest.ErrNotFound
		}
		return rs.repo.SourceDocument.PostSources(ctx, id, r.URL.Query().Get("after"), limit)
	})
}

func (rs *nativeArchiveRoutes) collectionDocuments(w http.ResponseWriter, r *http.Request) {
	rs.documentLocationPage(w, r, "sources")
}

func (rs *nativeArchiveRoutes) documentClaims(w http.ResponseWriter, r *http.Request) {
	rs.documentLocationPage(w, r, "claims")
}

func (rs *nativeArchiveRoutes) documentHeadHistory(w http.ResponseWriter, r *http.Request) {
	rs.documentLocationPage(w, r, "history")
}

func (rs *nativeArchiveRoutes) documentLocationPage(w http.ResponseWriter, r *http.Request, kind string) {
	limit, err := documentLimit(r)
	if err != nil {
		documentError(w, err)
		return
	}
	rs.readDocument(w, r, func(ctx context.Context) (any, error) {
		collection, path, after := chi.URLParam(r, "collection"), r.URL.Query().Get("path"), r.URL.Query().Get("after")
		if err := rs.documentLocation(ctx, collection, path); err != nil {
			return nil, err
		}
		switch kind {
		case "sources":
			return rs.repo.SourceDocument.LocationSources(ctx, collection, path, after, limit)
		case "claims":
			return rs.repo.SourceDocument.HeadClaims(ctx, collection, path, after, limit)
		default:
			revision := 0
			if after != "" {
				var err error
				revision, err = strconv.Atoi(after)
				if err != nil || revision < 0 {
					return nil, ingest.ErrInvalid
				}
			}
			return rs.repo.SourceDocument.HeadHistory(ctx, collection, path, revision, limit)
		}
	})
}

func (rs *nativeArchiveRoutes) documentHead(w http.ResponseWriter, r *http.Request) {
	rs.readDocument(w, r, func(ctx context.Context) (any, error) {
		collection, path := chi.URLParam(r, "collection"), r.URL.Query().Get("path")
		if err := rs.documentLocation(ctx, collection, path); err != nil {
			return nil, err
		}
		return rs.repo.SourceDocument.Head(ctx, collection, path)
	})
}

func (rs *nativeArchiveRoutes) putDocumentHead(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RelativePath     string `json:"relative_path"`
		ExpectedRevision int    `json:"expected_revision"`
		State            string `json:"state"`
		SourceUUID       string `json:"source_uuid"`
		ClaimUUID        string `json:"claim_uuid"`
		Reason           string `json:"reason"`
	}
	if err := readIngestJSON(w, r, 80<<10, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.SourceDocumentHead
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		collection := chi.URLParam(r, "collection")
		if err := rs.documentLocation(ctx, collection, input.RelativePath); err != nil {
			return err
		}
		var err error
		result, err = rs.repo.SourceDocument.DecideHead(ctx, models.SourceDocumentHeadInput{CollectionUUID: collection, RelativePath: input.RelativePath,
			ExpectedRevision: input.ExpectedRevision, State: input.State, SourceUUID: input.SourceUUID, ClaimUUID: input.ClaimUUID, Reason: input.Reason, Origin: "review"})
		return err
	})
	if err != nil {
		documentError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
