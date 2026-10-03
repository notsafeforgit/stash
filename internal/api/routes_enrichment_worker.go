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

const maxEnrichmentRequestBytes = archive.MaxEnrichmentTranscriptBytes + 4096

func (rs *ingestRoutes) enrichmentWorker() *ingest.EnrichmentCoordinator {
	if rs.enrichment != nil {
		return rs.enrichment
	}
	return ingest.NewEnrichmentCoordinator(rs.service)
}

func (rs *ingestRoutes) enrichmentRoutes(r chi.Router) {
	r.Route("/enrichment", func(r chi.Router) {
		// Authenticate before decoding a potentially large checkpoint. Domain
		// methods recheck authority and ownership in their own transaction.
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := rs.service.Authenticate(r.Context(), ingestToken(r)); err != nil {
					ingestError(w, err)
					return
				}
				for _, key := range []string{"job", "target", "collection"} {
					if id := chi.URLParam(r, key); id != "" && !ingest.ValidUUID(id) {
						ingestError(w, ingest.ErrInvalid)
						return
					}
				}
				next.ServeHTTP(w, r)
			})
		})
		r.Post("/collections/{collection}/ready", rs.readyEnrichment)
		r.Post("/targets/{target}/jobs", rs.admitEnrichment)
		r.Get("/jobs/{job}", rs.describeEnrichment)
		r.Post("/jobs/{job}/claim", rs.claimEnrichment)
		r.Post("/jobs/{job}/renew", rs.renewEnrichment)
		r.Get("/jobs/{job}/checkpoint", rs.readEnrichmentCheckpoint)
		r.Post("/jobs/{job}/checkpoint", rs.saveEnrichmentCheckpoint)
		r.Post("/jobs/{job}/publish", rs.publishEnrichment)
		r.Post("/jobs/{job}/failure", rs.failEnrichment)
		r.Get("/jobs/{job}/publication", rs.readEnrichmentPublication)
		r.Get("/jobs/{job}/release", rs.readEnrichmentRelease)
	})
}

func writeEnrichmentWorker(w http.ResponseWriter, value any, err error) {
	switch {
	case err == nil:
		// Keep source JSON in its bounded native representation. The default
		// HTML escaping could expand a valid 32 MiB checkpoint sixfold.
		body, err := archive.EncodeSourceJSON(value)
		if err != nil {
			ingestError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	case errors.Is(err, models.ErrArchiveJobLease):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "lease_lost"})
	case errors.Is(err, models.ErrArchiveJobConflict):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "enrichment_work_changed"})
	default:
		writeEnrichmentWork(w, value, err)
	}
}

func (rs *ingestRoutes) readyEnrichment(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Limit int `json:"limit"`
	}
	if err := readIngestJSON(w, r, 1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.enrichmentWorker().Ready(r.Context(), ingestToken(r), chi.URLParam(r, "collection"), input.Limit)
	writeEnrichmentWorker(w, value, err)
}

func (rs *ingestRoutes) admitEnrichment(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedRevision int    `json:"expected_revision"`
		PolicySHA256     string `json:"policy_sha256"`
		ExtractorVersion string `json:"extractor_version"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.enrichmentWorker().Admit(r.Context(), ingestToken(r), chi.URLParam(r, "target"), input.ExpectedRevision, input.PolicySHA256, input.ExtractorVersion)
	writeEnrichmentWorker(w, value, err)
}

func (rs *ingestRoutes) describeEnrichment(w http.ResponseWriter, r *http.Request) {
	value, err := rs.enrichmentWorker().Describe(r.Context(), ingestToken(r), chi.URLParam(r, "job"))
	writeEnrichmentWorker(w, value, err)
}

func (rs *ingestRoutes) claimEnrichment(w http.ResponseWriter, r *http.Request) {
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
	value, err := rs.enrichmentWorker().Claim(r.Context(), ingestToken(r), chi.URLParam(r, "job"), input.ExpectedRevision, input.OwnerUUID, input.PolicySHA256, input.ExtractorVersion, time.Duration(input.LeaseSeconds)*time.Second)
	if err == nil && value == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeEnrichmentWorker(w, value, err)
}

// Job identity comes from the route, producer identity from the credential.
type enrichmentLeaseInput struct {
	OwnerUUID string `json:"owner_uuid"`
	Fence     int64  `json:"fence"`
}

func (i enrichmentLeaseInput) lease(r *http.Request) models.ArchiveJobLease {
	return models.ArchiveJobLease{JobUUID: chi.URLParam(r, "job"), OwnerUUID: i.OwnerUUID, Fence: i.Fence}
}

func (rs *ingestRoutes) renewEnrichment(w http.ResponseWriter, r *http.Request) {
	var input struct {
		enrichmentLeaseInput
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
	value, err := rs.enrichmentWorker().Renew(r.Context(), ingestToken(r), input.lease(r), time.Duration(input.LeaseSeconds)*time.Second)
	writeEnrichmentWorker(w, value, err)
}

func (rs *ingestRoutes) readEnrichmentCheckpoint(w http.ResponseWriter, r *http.Request) {
	value, err := rs.enrichmentWorker().CheckpointHead(r.Context(), ingestToken(r), chi.URLParam(r, "job"))
	writeEnrichmentWorker(w, value, err)
}

func (rs *ingestRoutes) saveEnrichmentCheckpoint(w http.ResponseWriter, r *http.Request) {
	var input struct {
		enrichmentLeaseInput
		ExpectedRevision int             `json:"expected_revision"`
		Body             json.RawMessage `json:"body"`
	}
	if err := readIngestJSON(w, r, maxEnrichmentRequestBytes, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.enrichmentWorker().Checkpoint(r.Context(), ingestToken(r), input.lease(r), input.ExpectedRevision, input.Body)
	writeEnrichmentWorker(w, value, err)
}

func (rs *ingestRoutes) publishEnrichment(w http.ResponseWriter, r *http.Request) {
	var input struct {
		enrichmentLeaseInput
		Revision int    `json:"checkpoint_revision"`
		Digest   string `json:"checkpoint_sha256"`
	}
	if err := readIngestJSON(w, r, 1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.enrichmentWorker().Publish(r.Context(), ingestToken(r), input.lease(r), input.Revision, input.Digest)
	writeEnrichmentWorker(w, value, err)
}

func (rs *ingestRoutes) failEnrichment(w http.ResponseWriter, r *http.Request) {
	var input struct {
		enrichmentLeaseInput
		ErrorCode string `json:"error_code"`
	}
	if err := readIngestJSON(w, r, 1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.enrichmentWorker().Fail(r.Context(), ingestToken(r), input.lease(r), input.ErrorCode)
	writeEnrichmentWorker(w, value, err)
}

func (rs *ingestRoutes) readEnrichmentPublication(w http.ResponseWriter, r *http.Request) {
	value, err := rs.enrichmentWorker().Publication(r.Context(), ingestToken(r), chi.URLParam(r, "job"))
	writeEnrichmentWorker(w, value, err)
}

func (rs *ingestRoutes) readEnrichmentRelease(w http.ResponseWriter, r *http.Request) {
	value, err := rs.enrichmentWorker().CheckpointRelease(r.Context(), ingestToken(r), chi.URLParam(r, "job"))
	writeEnrichmentWorker(w, value, err)
}
