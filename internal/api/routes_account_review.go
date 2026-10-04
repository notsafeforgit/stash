package api

import (
	"context"
	"encoding/json"
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

func accountReviewPage(r *http.Request) (string, int, error) {
	limit := 25
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			return "", 0, ingest.ErrInvalid
		}
	}
	return r.URL.Query().Get("after"), limit, nil
}

func (rs *nativeArchiveRoutes) reviewAccounts(w http.ResponseWriter, r *http.Request) {
	after, limit, err := accountReviewPage(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	var result []models.AccountReviewState
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceAccount.ReviewAccounts(ctx, models.AccountReviewFilter{After: after, Limit: limit,
			Query: r.URL.Query().Get("q"), Namespace: r.URL.Query().Get("namespace"), Ownership: models.AccountOwnershipState(r.URL.Query().Get("ownership"))})
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) lookupReviewAccounts(w http.ResponseWriter, r *http.Request) {
	after, limit, err := accountReviewPage(r)
	reference, referenceErr := archive.NormalizeAccountReference(models.AccountReference{
		Namespace: r.URL.Query().Get("namespace"), Kind: r.URL.Query().Get("kind"), Value: r.URL.Query().Get("value"),
	})
	if err != nil || referenceErr != nil || (after != "" && !ingest.ValidUUID(after)) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	result := []models.AccountReviewState{}
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		accounts, err := rs.repo.SourceAccount.Lookup(ctx, reference, after, limit)
		if err != nil {
			return err
		}
		for _, account := range accounts {
			state, err := rs.repo.SourceAccount.ReviewAccount(ctx, account.UUID)
			if err != nil {
				return err
			}
			if state == nil {
				return models.ErrSourcePayloadCorrupt
			}
			result = append(result, *state)
		}
		return nil
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) reviewAccount(w http.ResponseWriter, r *http.Request) {
	var result *models.AccountReviewState
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceAccount.ReviewAccount(ctx, chi.URLParam(r, "account"))
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

func (rs *nativeArchiveRoutes) reviewAccountIdentifiers(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "account")
	after, limit, err := accountReviewPage(r)
	if err != nil || !ingest.ValidUUID(id) || (after != "" && !ingest.ValidUUID(after)) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	result := []models.AccountReviewIdentifier{}
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		account, err := rs.repo.SourceAccount.Find(ctx, id)
		if err != nil {
			return err
		}
		if account == nil {
			return ingest.ErrNotFound
		}
		identifiers, err := rs.repo.SourceAccount.Identifiers(ctx, id, after, limit)
		if err != nil {
			return err
		}
		for _, identifier := range identifiers {
			result = append(result, models.AccountReviewIdentifier{UUID: identifier.UUID, AccountUUID: identifier.AccountUUID, Reference: identifier.Reference})
		}
		return nil
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

type accountReviewEvidence struct {
	Key           string          `json:"key"`
	Basis         string          `json:"basis"`
	Origin        string          `json:"origin"`
	Details       json.RawMessage `json:"details"`
	FirstObserved time.Time       `json:"first_observed"`
	LastObserved  time.Time       `json:"last_observed"`
}

func (rs *nativeArchiveRoutes) reviewAccountEvidence(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "identifier")
	after, limit, err := accountReviewPage(r)
	if err != nil || !ingest.ValidUUID(id) || len(after) > 512 || !utf8.ValidString(after) || strings.IndexFunc(after, unicode.IsControl) >= 0 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	result := []accountReviewEvidence{}
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		evidence, err := rs.repo.SourceAccount.Evidence(ctx, id, after, limit)
		if err != nil {
			return err
		}
		for _, item := range evidence {
			result = append(result, accountReviewEvidence{Key: item.Key, Basis: item.Basis, Origin: item.Origin,
				Details: item.Details, FirstObserved: item.FirstObserved, LastObserved: item.LastObserved})
		}
		return nil
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) reviewAccountHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "account")
	cursor, limit, err := accountReviewPage(r)
	after := 0
	if cursor != "" && err == nil {
		after, err = strconv.Atoi(cursor)
	}
	if err != nil || after < 0 || !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []models.AccountReviewOwnership
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		account, err := rs.repo.SourceAccount.Find(ctx, id)
		if err != nil {
			return err
		}
		if account == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.SourceAccount.ReviewOwnershipHistory(ctx, id, after, limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) previewAccountOwnership(w http.ResponseWriter, r *http.Request) {
	var input models.AccountOwnershipReviewInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.AccountOwnershipPreview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceAccount.PreviewOwnership(ctx, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) applyAccountOwnership(w http.ResponseWriter, r *http.Request) {
	var input models.AccountOwnershipReviewApplyInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.AccountOwnershipReview
	var replayed bool
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, replayed, err = rs.repo.SourceAccount.ApplyOwnershipReview(ctx, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]any{"review": result, "replayed": replayed})
}

func (rs *nativeArchiveRoutes) accountOwnershipReview(w http.ResponseWriter, r *http.Request) {
	var result *models.AccountOwnershipReview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceAccount.OwnershipReview(ctx, chi.URLParam(r, "request"))
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
