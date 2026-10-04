package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeAccountReviewHTTPExplicitSelectionRecoveryAndDiscovery(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "account-review.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var input models.AccountOwnershipReviewInput
	var identifier *models.AccountIdentifier
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO performers(id,created_at,updated_at) VALUES(1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),(2,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO performer_names(performer_id,name,position) VALUES(1,'Shared name',0),(2,'Shared name',0);`, nil)
		if err != nil {
			return err
		}
		owner, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, 2)
		if err != nil {
			return err
		}
		now := time.Now()
		for i := 0; i < 2; i++ {
			account, err := repo.SourceAccount.Create(ctx, "native:reddit", "Shared account")
			if err != nil {
				return err
			}
			identifier, err = repo.SourceAccount.ObserveIdentifier(ctx, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "handle", Value: "shared"},
				models.AccountIdentifierEvidence{Key: "profile", Basis: "source-profile", Origin: "fixture", Details: json.RawMessage(`{"profile_url":"https://www.reddit.com/user/shared/"}`), FirstObserved: now, LastObserved: now})
			if err != nil {
				return err
			}
			if i == 1 {
				for n := 0; n < 9; n++ {
					_, err = repo.SourceAccount.ObserveIdentifier(ctx, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "id", Value: fmt.Sprintf("id%d", n)},
						models.AccountIdentifierEvidence{Key: "capture", Basis: "source-profile", Origin: "fixture", FirstObserved: now, LastObserved: now})
					if err != nil {
						return err
					}
				}
				account, err = repo.SourceAccount.Find(ctx, account.UUID)
				if err != nil {
					return err
				}
				input = models.AccountOwnershipReviewInput{AccountUUID: account.UUID, AccountRevision: account.Revision, State: models.AccountOwnershipLinked,
					PerformerUUID: owner.UUID, PerformerRevision: owner.Revision, Reason: "Explicit owner"}
			}
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
	getAccount := func() models.AccountReviewState {
		w := request(http.MethodGet, "/source-accounts/"+input.AccountUUID, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var result models.AccountReviewState
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		return result
	}
	account := getAccount()
	require.Nil(t, account.Ownership)
	require.True(t, account.MoreIdentifiers)
	require.Len(t, account.Identifiers, 8)
	w := request(http.MethodGet, "/source-accounts/lookup?namespace=native%3Areddit&kind=handle&value=Shared&limit=1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var candidates []models.AccountReviewState
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &candidates))
	require.Len(t, candidates, 1)
	firstID := candidates[0].UUID
	w = request(http.MethodGet, "/source-accounts/lookup?namespace=native%3Areddit&kind=handle&value=shared&limit=1&after="+firstID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &candidates))
	require.Len(t, candidates, 1)
	require.NotEqual(t, firstID, candidates[0].UUID, "a shared identifier remains ambiguous")
	w = request(http.MethodGet, "/source-accounts/"+input.AccountUUID+"/identifiers?limit=100", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var identifiers []models.AccountReviewIdentifier
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &identifiers))
	require.Len(t, identifiers, 10)
	w = request(http.MethodGet, "/source-account-identifiers/"+identifier.UUID+"/evidence", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var evidence []accountReviewEvidence
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &evidence))
	require.Len(t, evidence, 1)
	require.Equal(t, "profile", evidence[0].Key)
	require.JSONEq(t, `{"profile_url":"https://www.reddit.com/user/shared/"}`, string(evidence[0].Details))
	w = request(http.MethodGet, "/source-account-identifiers/"+identifier.UUID+"/evidence?after=profile", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, "[]", w.Body.String())
	w = request(http.MethodPost, "/account-ownership/preview", input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var preview models.AccountOwnershipPreview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	require.Equal(t, 2, *preview.Performer.LocalID)
	require.Nil(t, getAccount().Ownership, "preview cannot create ownership")
	apply := models.AccountOwnershipReviewApplyInput{AccountOwnershipReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
	var receipt models.AccountOwnershipReview
	for i := 0; i < 2; i++ {
		w = request(http.MethodPost, "/account-ownership/apply", apply)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var result struct {
			Review   models.AccountOwnershipReview `json:"review"`
			Replayed bool                          `json:"replayed"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Equal(t, i == 1, result.Replayed)
		receipt = result.Review
	}
	w = request(http.MethodGet, "/account-ownership/requests/"+apply.RequestUUID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var saved models.AccountOwnershipReview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.Equal(t, receipt, saved)
	require.Equal(t, 2, *getAccount().Ownership.Performer.LocalID)
	w = request(http.MethodGet, "/source-accounts?ownership=linked&q=shared&namespace=native%3Areddit", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &candidates))
	require.Len(t, candidates, 1)
	require.Equal(t, input.AccountUUID, candidates[0].UUID)
	changed := apply
	changed.Reason = "Changed request"
	w = request(http.MethodPost, "/account-ownership/apply", changed)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "request_conflict")
	changed = apply
	changed.RequestUUID = uuid.NewString()
	w = request(http.MethodPost, "/account-ownership/apply", changed)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "preview_changed")
	account = getAccount()
	unlink := models.AccountOwnershipReviewInput{AccountUUID: account.UUID, AccountRevision: account.Revision, State: models.AccountOwnershipUnlinked}
	w = request(http.MethodPost, "/account-ownership/preview", unlink)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	preview = models.AccountOwnershipPreview{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	w = request(http.MethodPost, "/account-ownership/apply", models.AccountOwnershipReviewApplyInput{AccountOwnershipReviewInput: unlink, RequestUUID: uuid.NewString(), Digest: preview.Digest})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = request(http.MethodPost, "/account-ownership/apply", apply)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, models.AccountOwnershipUnlinked, getAccount().Ownership.State, "old recovery must not restore a superseded owner")
	w = request(http.MethodGet, "/source-accounts/"+input.AccountUUID+"/ownership-history?limit=1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var history []models.AccountReviewOwnership
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &history))
	require.Len(t, history, 1)
	require.Equal(t, receipt.DecisionUUID, history[0].DecisionUUID)
	w = request(http.MethodGet, fmt.Sprintf("/source-accounts/%s/ownership-history?limit=1&after=%d", input.AccountUUID, history[0].Revision), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &history))
	require.Len(t, history, 1)
	require.Equal(t, models.AccountOwnershipUnlinked, history[0].State)

	for _, path := range []string{
		"/source-accounts?limit=101", "/source-accounts?limit=0", "/source-accounts?after=broken", "/source-accounts?ownership=auto",
		"/source-accounts?namespace=unqualified", "/source-accounts?" + url.Values{"q": {"bad\x00query"}}.Encode(),
		"/source-accounts/lookup?namespace=native%3Areddit&kind=handle", "/source-accounts/not-a-uuid",
		"/source-accounts/" + input.AccountUUID + "/identifiers?after=bad", "/source-accounts/" + input.AccountUUID + "/ownership-history?after=-1",
		"/source-account-identifiers/" + identifier.UUID + "/evidence?after=%00", "/account-ownership/requests/bad",
	} {
		w = request(http.MethodGet, path, nil)
		require.Equal(t, http.StatusBadRequest, w.Code, path+": "+w.Body.String())
	}
	for _, path := range []string{"/source-accounts/", "/account-ownership/requests/"} {
		w = request(http.MethodGet, path+uuid.NewString(), nil)
		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	}
	for _, body := range []string{`{"account_uuid":"x","unknown":true}`, `{"state":"linked","state":"unlinked"}`} {
		req := httptest.NewRequest(http.MethodPost, "/account-ownership/preview", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
	for _, path := range []string{"/source-accounts", "/account-ownership/preview", "/account-ownership/apply"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Origin", "https://unrelated.example")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	}
}
