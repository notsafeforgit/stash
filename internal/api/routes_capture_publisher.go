package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type publisherAccount struct {
	UUID          string `json:"uuid"`
	CanonicalUUID string `json:"canonical_uuid"`
	Namespace     string `json:"namespace"`
	Label         string `json:"label"`
	Revision      int    `json:"revision"`
}

func publisherAccountView(account *models.SourceAccount) *publisherAccount {
	if account == nil {
		return nil
	}
	return &publisherAccount{UUID: account.UUID, CanonicalUUID: account.CanonicalUUID,
		Namespace: account.Namespace, Label: account.Label, Revision: account.Revision}
}

type publisherDecision struct {
	RequestUUID          string    `json:"request_uuid"`
	CaptureUUID          string    `json:"capture_uuid"`
	Revision             int       `json:"revision"`
	State                string    `json:"state"`
	AccountUUID          *string   `json:"account_uuid"`
	CanonicalAccountUUID *string   `json:"canonical_account_uuid"`
	Origin               string    `json:"origin"`
	Policy               string    `json:"policy"`
	Reason               string    `json:"reason"`
	CreatedAt            time.Time `json:"created_at"`
}

func publisherDecisionView(decision *models.CapturePublisherDecision) *publisherDecision {
	if decision == nil {
		return nil
	}
	return &publisherDecision{RequestUUID: decision.UUID, CaptureUUID: decision.CaptureUUID,
		Revision: decision.Revision, State: decision.State, AccountUUID: decision.AccountUUID,
		CanonicalAccountUUID: decision.CanonicalAccountUUID, Origin: decision.Origin,
		Policy: decision.Policy, Reason: decision.Reason, CreatedAt: decision.CreatedAt}
}

type publisherCandidate struct {
	Account *publisherAccount         `json:"account"`
	Matches []models.AccountReference `json:"matches"`
}

type publisherObservation struct {
	CaptureUUID string     `json:"capture_uuid"`
	CapturedAt  *time.Time `json:"captured_at"`
}

type publisherPreview struct {
	CaptureUUID         string                  `json:"capture_uuid"`
	PostUUID            string                  `json:"post_uuid"`
	PostState           string                  `json:"post_state"`
	Current             *publisherDecision      `json:"current"`
	Identity            *models.CapturedAccount `json:"identity"`
	IdentityError       string                  `json:"identity_error,omitempty"`
	Candidates          []publisherCandidate    `json:"candidates"`
	StableIDMatches     bool                    `json:"stable_id_matches"`
	CandidatesTruncated bool                    `json:"candidates_truncated"`
	Target              *publisherAccount       `json:"target"`
	Action              string                  `json:"action"`
	AccountUUID         *string                 `json:"account_uuid"`
	Conflicts           []string                `json:"conflicts"`
	Signature           string                  `json:"signature"`
	Observation         *publisherObservation   `json:"observation,omitempty"`
}

func publisherPreviewView(preview *models.CapturePublisherPreview) publisherPreview {
	ret := publisherPreview{CaptureUUID: preview.CaptureUUID, PostUUID: preview.PostUUID,
		PostState: preview.PostState, Current: publisherDecisionView(preview.Current), Identity: preview.Identity,
		IdentityError: preview.IdentityError, Candidates: []publisherCandidate{}, StableIDMatches: preview.StableIDMatches,
		CandidatesTruncated: preview.CandidatesTruncated, Target: publisherAccountView(preview.Target),
		Action: preview.Action, AccountUUID: preview.AccountUUID, Conflicts: preview.Conflicts, Signature: preview.Signature}
	for _, candidate := range preview.Candidates {
		ret.Candidates = append(ret.Candidates, publisherCandidate{Account: publisherAccountView(&candidate.Account), Matches: candidate.Matches})
	}
	if preview.Observation != nil {
		ret.Observation = &publisherObservation{CaptureUUID: preview.Observation.CaptureUUID, CapturedAt: preview.Observation.CapturedAt}
	}
	return ret
}

func (rs *nativeArchiveRoutes) previewCapturePublisher(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CaptureUUID string `json:"capture_uuid"`
		AccountUUID string `json:"account_uuid,omitempty"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	if !ingest.ValidUUID(input.CaptureUUID) || (input.AccountUUID != "" && !ingest.ValidUUID(input.AccountUUID)) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.CapturePublisherPreview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		capture, err := rs.repo.SourceEvidence.FindCapture(ctx, input.CaptureUUID)
		if err != nil {
			return err
		}
		if capture == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.CapturePublisher.Preview(ctx, input.CaptureUUID, input.AccountUUID)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, publisherPreviewView(result))
}

type publisherApplyInput struct {
	RequestUUID string `json:"request_uuid"`
	CaptureUUID string `json:"capture_uuid"`
	Signature   string `json:"signature"`
	Action      string `json:"action"`
	AccountUUID string `json:"account_uuid,omitempty"`
	Reason      string `json:"reason"`
}

func (input publisherApplyInput) valid() bool {
	if !ingest.ValidUUID(input.RequestUUID) || !ingest.ValidUUID(input.CaptureUUID) || !archive.ValidSHA256(input.Signature) ||
		len(input.Reason) > 4096 || !utf8.ValidString(input.Reason) || strings.TrimSpace(input.Reason) != input.Reason || strings.ContainsFunc(input.Reason, unicode.IsControl) {
		return false
	}
	switch input.Action {
	case "link":
		return ingest.ValidUUID(input.AccountUUID)
	case "automatic", "create", "unlink", "inherit":
		return input.AccountUUID == ""
	default:
		return false
	}
}

func (rs *nativeArchiveRoutes) applyCapturePublisher(w http.ResponseWriter, r *http.Request) {
	var input publisherApplyInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	if !input.valid() {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	origin := "review"
	if input.Action == "automatic" {
		origin = "capture"
	}
	var decision *models.CapturePublisherDecision
	var replayed bool
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		previous, err := rs.repo.CapturePublisher.Request(ctx, input.RequestUUID)
		if err != nil {
			return err
		}
		replayed = previous != nil
		// Apply validates the entire original request before returning a receipt;
		// a later unlink or consolidation must not cause an old link to run again.
		decision, err = rs.repo.CapturePublisher.Apply(ctx, models.CapturePublisherInput{
			UUID: input.RequestUUID, CaptureUUID: input.CaptureUUID, ExpectedSignature: input.Signature,
			Action: input.Action, AccountUUID: input.AccountUUID, Origin: origin, Reason: input.Reason})
		return err
	})
	if errors.Is(err, models.ErrCapturePublisherReplay) {
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "request_conflict", "message": "This request UUID already names a different publisher choice."})
		return
	}
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]any{"decision": publisherDecisionView(decision), "replayed": replayed})
}

func (rs *nativeArchiveRoutes) capturePublisherRequest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "request")
	if !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.CapturePublisherDecision
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CapturePublisher.Request(ctx, id)
		if err == nil && result == nil {
			return ingest.ErrNotFound
		}
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, publisherDecisionView(result))
}

func (rs *nativeArchiveRoutes) capturePublisherHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "capture")
	cursor, limit, err := accountReviewPage(r)
	after := 0
	if cursor != "" && err == nil {
		after, err = strconv.Atoi(cursor)
	}
	if err != nil || after < 0 || !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	result := []*publisherDecision{}
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		capture, err := rs.repo.SourceEvidence.FindCapture(ctx, id)
		if err != nil {
			return err
		}
		if capture == nil {
			return ingest.ErrNotFound
		}
		decisions, err := rs.repo.CapturePublisher.History(ctx, id, after, limit)
		if err != nil {
			return err
		}
		for _, decision := range decisions {
			result = append(result, publisherDecisionView(&decision))
		}
		return nil
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
