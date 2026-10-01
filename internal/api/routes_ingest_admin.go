package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

// Mounted only on the application's authenticated router. Producer tokens do
// not authenticate application sessions or grant access to these endpoints.
func (rs *ingestRoutes) adminRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(nativeAdminOrigin)
	r.Post("/producers", rs.createProducer)
	r.Get("/producers", rs.producers)
	r.Post("/producers/{producer}/credentials", rs.issueCredential)
	r.Get("/producers/{producer}/credentials", rs.credentials)
	r.Delete("/credentials/{credential}", rs.revokeCredential)
	r.Post("/runs/{run}/review", rs.reviewRun)
	return r
}

func (rs *ingestRoutes) createProducer(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Label string `json:"label"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	if len(input.Label) == 0 || len(input.Label) > 256 || strings.TrimSpace(input.Label) != input.Label || strings.ContainsFunc(input.Label, unicode.IsControl) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var producer *models.IngestProducer
	err := rs.service.Repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		producer, err = rs.service.Repo.Ingest.CreateProducer(ctx, input.Label)
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusCreated, producer)
}

func (rs *ingestRoutes) producers(w http.ResponseWriter, r *http.Request) {
	after := r.URL.Query().Get("after")
	if after != "" && !ingest.ValidUUID(after) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []models.IngestProducer
	err := rs.service.Repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.service.Repo.Ingest.Producers(ctx, after, 50)
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *ingestRoutes) credentials(w http.ResponseWriter, r *http.Request) {
	producer, after := chi.URLParam(r, "producer"), r.URL.Query().Get("after")
	if !ingest.ValidUUID(producer) || (after != "" && !ingest.ValidUUID(after)) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []models.IngestCredential
	err := rs.service.Repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.service.Repo.Ingest.Credentials(ctx, producer, after, 50)
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *ingestRoutes) issueCredential(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Scopes    []models.IngestScope `json:"scopes"`
		ExpiresAt *time.Time           `json:"expires_at"`
	}
	if err := readIngestJSON(w, r, 65536, &input); err != nil {
		ingestError(w, err)
		return
	}
	if len(input.Scopes) < 1 || len(input.Scopes) > 128 || (input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now())) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	credential, token, err := rs.service.IssueCredential(r.Context(), chi.URLParam(r, "producer"), input.Scopes, input.ExpiresAt)
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusCreated, map[string]interface{}{"credential": credential, "token": token})
}

func (rs *ingestRoutes) revokeCredential(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "credential")
	if !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	err := rs.service.Repo.WithTxn(r.Context(), func(ctx context.Context) error { return rs.service.Repo.Ingest.RevokeCredential(ctx, id) })
	if err != nil {
		ingestError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func nativeAdminOrigin(next http.Handler) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			ingestError(w, ingest.ErrForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
				ingestError(w, ingest.ErrForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})

}
