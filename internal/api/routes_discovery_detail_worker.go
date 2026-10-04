package api

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"net/http"
	"time"
)

func (rs *ingestRoutes) detailWorker() *ingest.DiscoveryDetailCoordinator {
	if rs.detail != nil {
		return rs.detail
	}
	return ingest.NewDiscoveryDetailCoordinator(rs.service)
}
func (rs *ingestRoutes) discoveryDetailRoutes(r chi.Router) {
	r.Route("/discovery-details", func(r chi.Router) {
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
		r.Post("/targets/{target}/jobs", rs.admitDiscoveryDetail)
		r.Post("/collections/ready", rs.readyDiscoveryDetailCollections)
		r.Post("/collections/inspect", rs.inspectDiscoveryDetailCollections)
		r.Post("/collections/{collection}/candidates", rs.discoveryDetailCandidates)
		r.Post("/collections/{collection}/jobs/ready", rs.readyDiscoveryDetails)
		r.Get("/jobs/{job}", rs.readDiscoveryDetail)
		r.Post("/jobs/{job}/retry", rs.retryDiscoveryDetail)
		r.Post("/jobs/{job}/claim", rs.claimDiscoveryDetail)
		r.Post("/jobs/{job}/renew", rs.renewDiscoveryDetail)
		r.Post("/jobs/{job}/source", rs.reserveDiscoveryDetailSource)
		r.Post("/jobs/{job}/checkpoint", rs.saveDiscoveryDetailCheckpoint)
		r.Get("/jobs/{job}/checkpoint", rs.readDiscoveryDetailCheckpoint)
		r.Post("/jobs/{job}/complete", rs.completeDiscoveryDetail)
		r.Get("/jobs/{job}/result", rs.readDiscoveryDetailResult)
		r.Post("/jobs/{job}/failure", rs.failDiscoveryDetail)
	})
}
func (rs *ingestRoutes) readyDiscoveryDetailCollections(w http.ResponseWriter, r *http.Request) {
	rs.discoveryDetailCollections(w, r, false)
}
func (rs *ingestRoutes) inspectDiscoveryDetailCollections(w http.ResponseWriter, r *http.Request) {
	rs.discoveryDetailCollections(w, r, true)
}
func (rs *ingestRoutes) discoveryDetailCollections(w http.ResponseWriter, r *http.Request, inspect bool) {
	var input struct {
		PolicySHA256     string `json:"policy_sha256"`
		ExtractorVersion string `json:"extractor_version"`
		After            string `json:"after"`
		Limit            int    `json:"limit"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	var value []models.EnrichmentCollectionCandidate
	var err error
	if inspect {
		value, err = rs.detailWorker().InspectionCollections(r.Context(), ingestToken(r), input.PolicySHA256, input.ExtractorVersion, input.After, input.Limit)
	} else {
		value, err = rs.detailWorker().ReadyCollections(r.Context(), ingestToken(r), input.PolicySHA256, input.ExtractorVersion, input.After, input.Limit)
	}
	writeDiscoveryWorker(w, value, err)
}
func (rs *ingestRoutes) discoveryDetailCandidates(w http.ResponseWriter, r *http.Request) {
	var input struct {
		After *models.DiscoveryDetailCursor `json:"after"`
		Limit int                           `json:"limit"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.detailWorker().Candidates(r.Context(), ingestToken(r), chi.URLParam(r, "collection"), input.After, input.Limit)
	writeDiscoveryWorker(w, value, err)
}
func (rs *ingestRoutes) admitDiscoveryDetail(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedTargetRevision int    `json:"expected_target_revision"`
		CandidateSequence      int64  `json:"candidate_sequence"`
		PolicySHA256           string `json:"policy_sha256"`
		ExtractorVersion       string `json:"extractor_version"`
		Automatic              bool   `json:"automatic"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.detailWorker().Admit(r.Context(), ingestToken(r), models.DiscoveryDetailAdmission{TargetUUID: chi.URLParam(r, "target"), ExpectedTargetRevision: input.ExpectedTargetRevision,
		CandidateSequence: input.CandidateSequence, PolicySHA256: input.PolicySHA256, ExtractorVersion: input.ExtractorVersion, Automatic: input.Automatic})
	writeDiscoveryWorker(w, value, err)
}
func (rs *ingestRoutes) retryDiscoveryDetail(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if err := readIngestJSON(w, r, 1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.detailWorker().Retry(r.Context(), ingestToken(r), chi.URLParam(r, "job"))
	writeDiscoveryWorker(w, value, err)
}
func (rs *ingestRoutes) readDiscoveryDetail(w http.ResponseWriter, r *http.Request) {
	value, err := rs.detailWorker().Find(r.Context(), ingestToken(r), chi.URLParam(r, "job"))
	writeDiscoveryWorker(w, value, err)
}
func (rs *ingestRoutes) readDiscoveryDetailResult(w http.ResponseWriter, r *http.Request) {
	value, err := rs.detailWorker().Result(r.Context(), ingestToken(r), chi.URLParam(r, "job"))
	writeDiscoveryWorker(w, value, err)
}
func (rs *ingestRoutes) readyDiscoveryDetails(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PolicySHA256     string `json:"policy_sha256"`
		ExtractorVersion string `json:"extractor_version"`
		After            int64  `json:"after"`
		Limit            int    `json:"limit"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.detailWorker().ReadyJobs(r.Context(), ingestToken(r), chi.URLParam(r, "collection"), input.PolicySHA256, input.ExtractorVersion, input.After, input.Limit)
	writeDiscoveryWorker(w, value, err)
}
func (rs *ingestRoutes) claimDiscoveryDetail(w http.ResponseWriter, r *http.Request) {
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
	value, err := rs.detailWorker().Claim(r.Context(), ingestToken(r), chi.URLParam(r, "job"), input.ExpectedRevision, input.OwnerUUID, input.PolicySHA256, input.ExtractorVersion, time.Duration(input.LeaseSeconds)*time.Second)
	if err == nil && value == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeDiscoveryWorker(w, value, err)
}

func (rs *ingestRoutes) renewDiscoveryDetail(w http.ResponseWriter, r *http.Request) {
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
	value, err := rs.detailWorker().Renew(r.Context(), ingestToken(r), input.lease(r), time.Duration(input.LeaseSeconds)*time.Second)
	writeDiscoveryWorker(w, value, err)
}

func (rs *ingestRoutes) readDiscoveryDetailCheckpoint(w http.ResponseWriter, r *http.Request) {
	value, err := rs.detailWorker().CheckpointHead(r.Context(), ingestToken(r), chi.URLParam(r, "job"))
	writeDiscoveryWorker(w, value, err)
}

func (rs *ingestRoutes) reserveDiscoveryDetailSource(w http.ResponseWriter, r *http.Request) {
	var input struct {
		discoveryLeaseInput
		URL string `json:"url"`
	}
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	ready, err := rs.detailWorker().ReserveSource(r.Context(), ingestToken(r), input.lease(r), input.URL)
	writeDiscoveryWorker(w, map[string]any{"job_uuid": chi.URLParam(r, "job"), "fence": input.Fence, "ready": ready}, err)
}

func (rs *ingestRoutes) saveDiscoveryDetailCheckpoint(w http.ResponseWriter, r *http.Request) {
	var input struct {
		discoveryLeaseInput
		ExpectedRevision int             `json:"expected_revision"`
		Body             json.RawMessage `json:"body"`
	}
	if err := readIngestJSON(w, r, maxEnrichmentRequestBytes, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.detailWorker().Checkpoint(r.Context(), ingestToken(r), input.lease(r), input.ExpectedRevision, input.Body)
	writeDiscoveryWorker(w, value, err)
}

func (rs *ingestRoutes) completeDiscoveryDetail(w http.ResponseWriter, r *http.Request) {
	var input struct {
		discoveryLeaseInput
		Revision int    `json:"checkpoint_revision"`
		Digest   string `json:"checkpoint_sha256"`
	}
	if err := readIngestJSON(w, r, 1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.detailWorker().Complete(r.Context(), ingestToken(r), input.lease(r), input.Revision, input.Digest)
	writeDiscoveryWorker(w, value, err)
}

func (rs *ingestRoutes) failDiscoveryDetail(w http.ResponseWriter, r *http.Request) {
	var input struct {
		discoveryLeaseInput
		ErrorCode string `json:"error_code"`
	}
	if err := readIngestJSON(w, r, 1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.detailWorker().Fail(r.Context(), ingestToken(r), input.lease(r), input.ErrorCode)
	writeDiscoveryWorker(w, value, err)
}
