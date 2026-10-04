package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeAccountConsolidationReviewHTTP(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "consolidation-review.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	input := models.AccountConsolidationReviewInput{OwnershipMode: "preserve"}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for _, target := range []*string{&input.SourceUUID, &input.DestinationUUID} {
			account, err := repo.SourceAccount.Create(ctx, "native:reddit", "Shared account")
			if err != nil {
				return err
			}
			*target = account.UUID
		}
		return nil
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		return result
	}
	w := request(http.MethodPost, "/account-consolidation/preview", input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var preview models.AccountConsolidationReviewPreview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	require.True(t, preview.Ready)
	require.Equal(t, input, preview.Input)
	require.Equal(t, input.SourceUUID, preview.Source.UUID)
	require.Equal(t, input.DestinationUUID, preview.Destination.UUID)
	apply := models.AccountConsolidationReviewApplyInput{AccountConsolidationReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
	checkPath := "/account-consolidation/requests/" + apply.RequestUUID + "/check"
	w = request(http.MethodPost, checkPath, apply)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	w = request(http.MethodGet, "/source-accounts/"+input.SourceUUID+"/consolidation-history", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, "[]", w.Body.String())
	var receipt models.AccountConsolidationReview
	for i := 0; i < 2; i++ {
		w = request(http.MethodPost, "/account-consolidation/apply", apply)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var result struct {
			Review   models.AccountConsolidationReview `json:"review"`
			Replayed bool                              `json:"replayed"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Equal(t, i == 1, result.Replayed)
		receipt = result.Review
	}
	w = request(http.MethodPost, checkPath, apply)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var recovered models.AccountConsolidationReview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recovered))
	require.Equal(t, receipt, recovered)
	w = request(http.MethodGet, "/source-accounts/"+input.DestinationUUID+"/consolidation-history?limit=1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var history []models.AccountConsolidationRecord
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &history))
	require.Equal(t, []models.AccountConsolidationRecord{receipt.Consolidation}, history)
	w = request(http.MethodGet, "/source-accounts/"+input.SourceUUID+"/consolidation-history?after=1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, "[]", w.Body.String())
	changed := apply
	changed.Reason = "Another choice"
	for _, path := range []string{checkPath, "/account-consolidation/apply"} {
		w = request(http.MethodPost, path, changed)
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "request_conflict")
	}
	changed = apply
	changed.RequestUUID = uuid.NewString()
	w = request(http.MethodPost, checkPath, changed)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	w = request(http.MethodPost, "/account-consolidation/apply", changed)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "preview_changed")
	for _, bad := range []models.AccountConsolidationReviewInput{
		{SourceUUID: input.SourceUUID, DestinationUUID: input.SourceUUID, OwnershipMode: "preserve"},
		{SourceUUID: input.SourceUUID, DestinationUUID: input.DestinationUUID, OwnershipMode: "choose"},
		{SourceUUID: input.SourceUUID, DestinationUUID: input.DestinationUUID, OwnershipMode: "preserve", Ownership: &models.AccountConsolidationChoice{State: models.AccountOwnershipUndecided}},
		{SourceUUID: input.SourceUUID, DestinationUUID: input.DestinationUUID, OwnershipMode: "choose", Ownership: &models.AccountConsolidationChoice{State: models.AccountOwnershipLinked}},
		{SourceUUID: input.SourceUUID, DestinationUUID: input.DestinationUUID, OwnershipMode: "preserve", Reason: "bad\nreason"},
		{SourceUUID: strings.ToUpper(input.SourceUUID), DestinationUUID: input.DestinationUUID, OwnershipMode: "preserve"},
	} {
		w = request(http.MethodPost, "/account-consolidation/preview", bad)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
	for _, body := range []string{`{"source_uuid":"x","unknown":true}`, `{"ownership_mode":"preserve","ownership_mode":"choose"}`} {
		req := httptest.NewRequest(http.MethodPost, "/account-consolidation/preview", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
	for _, path := range []string{checkPath, "/account-consolidation/preview", "/account-consolidation/apply"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Origin", "https://unrelated.example")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	}
}
