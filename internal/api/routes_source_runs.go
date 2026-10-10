package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *ingestRoutes) runCoordinator() *ingest.RunCoordinator {
	if rs.runs != nil {
		return rs.runs
	}
	return ingest.NewRunCoordinator(rs.service)
}

func sourceRunResponse(w http.ResponseWriter, r *models.SourceRun, err error) {
	if err != nil {
		ingestError(w, err)
		return
	}
	if r == nil {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ingestJSON(w, http.StatusOK, r)
}

func (rs *ingestRoutes) submitRun(w http.ResponseWriter, r *http.Request) {
	var input models.SourceRunRequest
	if err := readIngestJSON(w, r, 8192, &input); err != nil {
		ingestError(w, err)
		return
	}
	result, err := rs.runCoordinator().Submit(r.Context(), ingestToken(r), input)
	if err != nil {
		ingestError(w, err)
		return
	}
	// Many immutable requests can coalesce into one run. Echo the committed
	// request identity so a producer can release exactly that queued submission.
	ingestJSON(w, http.StatusOK, struct {
		*models.SourceRun
		RequestUUID string `json:"request_uuid"`
	}{result, input.RequestUUID})
}
func (rs *ingestRoutes) sourceRun(w http.ResponseWriter, r *http.Request) {
	result, err := rs.runCoordinator().Find(r.Context(), ingestToken(r), chi.URLParam(r, "run"))
	sourceRunResponse(w, result, err)
}
func (rs *ingestRoutes) sourceRuns(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CollectionUUID string `json:"collection_uuid"`
		After          int64  `json:"after"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	result, err := rs.runCoordinator().List(r.Context(), ingestToken(r), input.CollectionUUID, input.After)
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
func (rs *ingestRoutes) readySourceRuns(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RootUUID     string `json:"root_uuid"`
		PolicySHA256 string `json:"policy_sha256"`
		After        int64  `json:"after"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	result, err := rs.runCoordinator().Ready(r.Context(), ingestToken(r), input.RootUUID, input.PolicySHA256, input.After)
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
func (rs *ingestRoutes) sourceRunAttempts(w http.ResponseWriter, r *http.Request) {
	var input struct {
		After int64 `json:"after"`
	}
	if err := readIngestJSON(w, r, 1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	result, err := rs.runCoordinator().Attempts(r.Context(), ingestToken(r), chi.URLParam(r, "run"), input.After)
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
func (rs *ingestRoutes) claimRun(w http.ResponseWriter, r *http.Request) {
	var input struct {
		OwnerUUID        string `json:"owner_uuid"`
		PolicySHA256     string `json:"policy_sha256"`
		LeaseSeconds     int    `json:"lease_seconds"`
		RecoveryProtocol int    `json:"recovery_protocol"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	if input.LeaseSeconds < 5 || input.LeaseSeconds > 900 || input.RecoveryProtocol < 0 || input.RecoveryProtocol > 1 {
		ingestError(w, models.ErrSourceRunInvalid)
		return
	}
	if input.RecoveryProtocol == 0 {
		// Older workers ignore the archive replay policy. They must not claim
		// recovered work and report success after an ordinary archive stop.
		run, err := rs.runCoordinator().Find(r.Context(), ingestToken(r), chi.URLParam(r, "run"))
		if err != nil {
			ingestError(w, err)
			return
		}
		if run != nil && run.Recovery != nil {
			ingestError(w, models.ErrSourceRunConflict)
			return
		}
	}
	result, err := rs.runCoordinator().Claim(r.Context(), ingestToken(r), chi.URLParam(r, "run"), input.OwnerUUID, input.PolicySHA256, time.Duration(input.LeaseSeconds)*time.Second)
	sourceRunResponse(w, result, err)
}

func (rs *ingestRoutes) reserveRunSource(w http.ResponseWriter, r *http.Request) {
	var input struct {
		OwnerUUID string `json:"owner_uuid"`
		Fence     int64  `json:"fence"`
		URL       string `json:"url"`
	}
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	lease := models.SourceRunLease{RunUUID: chi.URLParam(r, "run"), OwnerUUID: input.OwnerUUID, Fence: input.Fence}
	result, err := rs.runCoordinator().ReserveSource(r.Context(), ingestToken(r), lease, input.URL)
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *ingestRoutes) changeRunLease(w http.ResponseWriter, r *http.Request) {
	var input struct {
		OwnerUUID    string                    `json:"owner_uuid"`
		Fence        int64                     `json:"fence"`
		LeaseSeconds *int                      `json:"lease_seconds,omitempty"`
		Progress     *models.SourceRunProgress `json:"progress,omitempty"`
		Outcome      *models.SourceRunOutcome  `json:"outcome,omitempty"`
	}
	if err := readIngestJSON(w, r, 8192, &input); err != nil {
		ingestError(w, err)
		return
	}
	count := 0
	if input.LeaseSeconds != nil {
		count++
	}
	if input.Progress != nil {
		count++
	}
	if input.Outcome != nil {
		count++
	}
	if count != 1 {
		ingestError(w, models.ErrSourceRunInvalid)
		return
	}
	lease := models.SourceRunLease{RunUUID: chi.URLParam(r, "run"), OwnerUUID: input.OwnerUUID, Fence: input.Fence}
	coordinator := rs.runCoordinator()
	var result *models.SourceRun
	var err error
	switch {
	case input.LeaseSeconds != nil:
		if *input.LeaseSeconds < 5 || *input.LeaseSeconds > 900 {
			ingestError(w, models.ErrSourceRunInvalid)
			return
		}
		result, err = coordinator.Renew(r.Context(), ingestToken(r), lease, time.Duration(*input.LeaseSeconds)*time.Second)
	case input.Progress != nil:
		result, err = coordinator.Progress(r.Context(), ingestToken(r), lease, *input.Progress)
	case input.Outcome != nil:
		result, err = coordinator.Finish(r.Context(), ingestToken(r), lease, *input.Outcome)
	}
	sourceRunResponse(w, result, err)
}

// Retry/cancel require application authentication, not producer authority.
func (rs *ingestRoutes) reviewRun(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedRevision int64  `json:"expected_revision"`
		Action           string `json:"action"`
	}
	if err := readIngestJSON(w, r, 1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.SourceRun
	err := rs.service.Repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.service.Repo.SourceRun.Review(ctx, chi.URLParam(r, "run"), input.ExpectedRevision, input.Action, time.Now())
		return err
	})
	sourceRunResponse(w, result, err)
}
