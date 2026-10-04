package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const maxDiscoveryRequestBytes = archive.MaxDiscoveryPageBytes + 4096

func (rs *ingestRoutes) discoveryWorker() *ingest.DiscoveryCoordinator {
	if rs.discovery != nil {
		return rs.discovery
	}
	return ingest.NewDiscoveryCoordinator(rs.service)
}

func (rs *ingestRoutes) discoveryRoutes(r chi.Router) {
	r.Route("/discovery", func(r chi.Router) {
		// Authenticate before reading a page body. The coordinator checks its
		// collection/root scope and original attempt again inside the transaction.
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := rs.service.Authenticate(r.Context(), ingestToken(r)); err != nil {
					ingestError(w, err)
					return
				}
				for _, key := range []string{"job", "listing"} {
					if id := chi.URLParam(r, key); id != "" && !ingest.ValidUUID(id) {
						ingestError(w, ingest.ErrInvalid)
						return
					}
				}
				next.ServeHTTP(w, r)
			})
		})
		r.Post("/listings/{listing}/jobs", rs.admitDiscovery)
		r.Get("/jobs/{job}", rs.describeDiscovery)
		r.Post("/jobs/{job}/claim", rs.claimDiscovery)
		r.Post("/jobs/{job}/renew", rs.renewDiscovery)
		r.Post("/jobs/{job}/source", rs.reserveDiscoverySource)
		r.Post("/jobs/{job}/page", rs.saveDiscoveryPage)
		r.Post("/jobs/{job}/failure", rs.failDiscovery)
	})
}

func writeDiscoveryWorker(w http.ResponseWriter, value any, err error) {
	switch {
	case err == nil:
		body, err := archive.EncodeSourceJSON(value)
		if err != nil {
			ingestError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	case errors.Is(err, models.ErrDiscoveryInvalid):
		ingestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_discovery_work"})
	case errors.Is(err, models.ErrArchiveJobLease):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "lease_lost"})
	case errors.Is(err, models.ErrDiscoveryConflict), errors.Is(err, models.ErrArchiveJobConflict):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "discovery_work_changed"})
	default:
		ingestError(w, err)
	}
}

func (rs *ingestRoutes) admitDiscovery(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedDefinitionSHA256 string `json:"expected_definition_sha256"`
		PolicySHA256             string `json:"policy_sha256"`
		ExtractorVersion         string `json:"extractor_version"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.discoveryWorker().Admit(r.Context(), ingestToken(r), chi.URLParam(r, "listing"), input.ExpectedDefinitionSHA256, input.PolicySHA256, input.ExtractorVersion)
	writeDiscoveryWorker(w, value, err)
}

func (rs *ingestRoutes) describeDiscovery(w http.ResponseWriter, r *http.Request) {
	value, err := rs.discoveryWorker().Describe(r.Context(), ingestToken(r), chi.URLParam(r, "job"))
	writeDiscoveryWorker(w, value, err)
}

func (rs *ingestRoutes) claimDiscovery(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedRevision int64  `json:"expected_revision"`
		OwnerUUID        string `json:"owner_uuid"`
		PolicySHA256     string `json:"policy_sha256"`
		ExtractorVersion string `json:"extractor_version"`
		LeaseSeconds     int    `json:"lease_seconds"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	if input.LeaseSeconds < 5 || input.LeaseSeconds > 900 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	value, err := rs.discoveryWorker().Claim(r.Context(), ingestToken(r), chi.URLParam(r, "job"), input.ExpectedRevision, input.OwnerUUID, input.PolicySHA256, input.ExtractorVersion, time.Duration(input.LeaseSeconds)*time.Second)
	if err == nil && value == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeDiscoveryWorker(w, value, err)
}

// The URL identifies the job; the bearer credential identifies the producer.
type discoveryLeaseInput struct {
	OwnerUUID string `json:"owner_uuid"`
	Fence     int64  `json:"fence"`
}

func (i discoveryLeaseInput) lease(r *http.Request) models.ArchiveJobLease {
	return models.ArchiveJobLease{JobUUID: chi.URLParam(r, "job"), OwnerUUID: i.OwnerUUID, Fence: i.Fence}
}

func (rs *ingestRoutes) renewDiscovery(w http.ResponseWriter, r *http.Request) {
	var input struct {
		discoveryLeaseInput
		LeaseSeconds int `json:"lease_seconds"`
	}
	if err := readIngestJSON(w, r, 1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	if input.LeaseSeconds < 5 || input.LeaseSeconds > 900 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	value, err := rs.discoveryWorker().Renew(r.Context(), ingestToken(r), input.lease(r), time.Duration(input.LeaseSeconds)*time.Second)
	writeDiscoveryWorker(w, value, err)
}

func (rs *ingestRoutes) reserveDiscoverySource(w http.ResponseWriter, r *http.Request) {
	var input struct {
		discoveryLeaseInput
		URL string `json:"url"`
	}
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	ready, err := rs.discoveryWorker().ReserveSource(r.Context(), ingestToken(r), input.lease(r), input.URL)
	writeDiscoveryWorker(w, map[string]any{"job_uuid": chi.URLParam(r, "job"), "fence": input.Fence, "ready": ready}, err)
}

func (rs *ingestRoutes) saveDiscoveryPage(w http.ResponseWriter, r *http.Request) {
	var input struct {
		discoveryLeaseInput
		Ordinal int             `json:"ordinal"`
		Body    json.RawMessage `json:"body"`
	}
	if err := readIngestJSON(w, r, maxDiscoveryRequestBytes, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.discoveryWorker().AppendPage(r.Context(), ingestToken(r), input.lease(r), input.Ordinal, input.Body)
	writeDiscoveryWorker(w, value, err)
}

func (rs *ingestRoutes) failDiscovery(w http.ResponseWriter, r *http.Request) {
	var input struct {
		discoveryLeaseInput
		ErrorCode string `json:"error_code"`
	}
	if err := readIngestJSON(w, r, 1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.discoveryWorker().Fail(r.Context(), ingestToken(r), input.lease(r), input.ErrorCode)
	writeDiscoveryWorker(w, value, err)
}
