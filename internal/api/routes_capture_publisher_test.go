package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func publisherHTTPAccount(t *testing.T, repo models.Repository, namespace, kind, value string) *models.SourceAccount {
	t.Helper()
	var account *models.SourceAccount
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		account, err = repo.SourceAccount.Create(ctx, namespace, "Imported account")
		if err != nil {
			return err
		}
		stamp := time.Now().UTC()
		_, err = repo.SourceAccount.ObserveIdentifier(ctx, account.UUID,
			models.AccountReference{Namespace: namespace, Kind: kind, Value: value},
			models.AccountIdentifierEvidence{Key: "imported", Basis: "legacy-locator", Origin: "migration",
				FirstObserved: stamp, LastObserved: stamp, Details: []byte(`{}`)})
		return err
	}))
	return account
}

func publisherHTTPCapture(t *testing.T, repo models.Repository) *models.SourceCapture {
	t.Helper()
	var capture *models.SourceCapture
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		post, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: uuid.NewString()}, "")
		if err != nil {
			return err
		}
		payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", []byte(`{"category":"reddit","author":"Example","author_fullname":"t2_abc123"}`))
		if err != nil {
			return err
		}
		capture, err = repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID,
			Origin: "gallery-dl", Platform: "reddit", CapturedAt: time.Now().UTC(), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload})
		return err
	}))
	return capture
}

func publisherHTTPSend(t *testing.T, handler http.Handler) func(string, any, int) *httptest.ResponseRecorder {
	t.Helper()
	return func(path string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		return w
	}
}

func TestCapturePublisherHTTPConfirmIDAndResolveLaterCaptures(t *testing.T) {
	db, repo, get := sourcePostBrowserHTTPFixture(t)
	account := publisherHTTPAccount(t, repo, "native:reddit", "handle", "example")
	capture := publisherHTTPCapture(t, repo)
	send := publisherHTTPSend(t, (&nativeArchiveRoutes{repo: repo}).router())
	preview := func(capture, account string) publisherPreview {
		t.Helper()
		var result publisherPreview
		require.NoError(t, json.Unmarshal(send("/capture-publisher/preview", map[string]string{
			"capture_uuid": capture, "account_uuid": account}, 200).Body.Bytes(), &result))
		return result
	}
	initial := preview(capture.UUID, "")
	require.Equal(t, "review", initial.Action)
	require.Contains(t, initial.Conflicts, "locator_only_candidates")
	require.Nil(t, initial.Current)
	require.Len(t, initial.Candidates, 1)
	require.Equal(t, account.UUID, initial.Candidates[0].Account.UUID)
	selected := preview(capture.UUID, account.UUID)
	require.Equal(t, account.UUID, selected.Target.UUID)
	input := publisherApplyInput{RequestUUID: uuid.NewString(), CaptureUUID: capture.UUID,
		Signature: selected.Signature, Action: "link", AccountUUID: account.UUID, Reason: "Confirm the captured account ID"}
	var first struct {
		Decision publisherDecision `json:"decision"`
		Replayed bool              `json:"replayed"`
	}
	require.NoError(t, json.Unmarshal(send("/capture-publisher/apply", input, 200).Body.Bytes(), &first))
	require.False(t, first.Replayed)
	require.Equal(t, "linked", first.Decision.State)
	require.Equal(t, "review", first.Decision.Origin)
	require.Equal(t, account.UUID, *first.Decision.CanonicalAccountUUID)
	var identifiers []models.AccountReviewIdentifier
	require.NoError(t, json.Unmarshal(get("/source-accounts/"+account.UUID+"/identifiers", 200).Body.Bytes(), &identifiers))
	require.Len(t, identifiers, 2)
	for _, id := range identifiers {
		if id.Reference.Kind == "id" {
			require.Equal(t, "t2_abc123", id.Reference.Value)
			var evidence []accountReviewEvidence
			require.NoError(t, json.Unmarshal(get("/source-account-identifiers/"+id.UUID+"/evidence", 200).Body.Bytes(), &evidence))
			require.Len(t, evidence, 1)
			require.Contains(t, string(evidence[0].Details), capture.UUID)
			require.Equal(t, capture.CapturedAt, evidence[0].FirstObserved)
		}
	}
	later := publisherHTTPCapture(t, repo)
	automatic := preview(later.UUID, "")
	require.Equal(t, "link", automatic.Action)
	require.Equal(t, account.UUID, *automatic.AccountUUID)
	send("/capture-publisher/apply", publisherApplyInput{RequestUUID: uuid.NewString(), CaptureUUID: later.UUID,
		Signature: automatic.Signature, Action: "automatic"}, 200)
	require.Contains(t, get("/posts/"+later.PostUUID+"/publishers", 200).Body.String(), account.UUID)
	var ownership models.AccountReviewState
	require.NoError(t, json.Unmarshal(get("/source-accounts/"+account.UUID, 200).Body.Bytes(), &ownership))
	require.Nil(t, ownership.Ownership, "publisher confirmation cannot infer a performer")

	changed := input
	changed.Reason = "A different request"
	require.Contains(t, send("/capture-publisher/apply", changed, 409).Body.String(), "request_conflict")
	stale := input
	stale.RequestUUID = uuid.NewString()
	send("/capture-publisher/apply", stale, 409)
	current := preview(capture.UUID, "")
	require.Equal(t, "preserve", current.Action)
	send("/capture-publisher/apply", publisherApplyInput{RequestUUID: uuid.NewString(), CaptureUUID: capture.UUID,
		Signature: current.Signature, Action: "unlink", Reason: "Explicit later choice"}, 200)
	var replay struct {
		Decision publisherDecision `json:"decision"`
		Replayed bool              `json:"replayed"`
	}
	require.NoError(t, json.Unmarshal(send("/capture-publisher/apply", input, 200).Body.Bytes(), &replay))
	require.True(t, replay.Replayed)
	require.Equal(t, first.Decision, replay.Decision)
	var receipt publisherDecision
	require.NoError(t, json.Unmarshal(get("/capture-publisher/requests/"+input.RequestUUID, 200).Body.Bytes(), &receipt))
	require.Equal(t, first.Decision, receipt)
	require.Equal(t, "unlinked", preview(capture.UUID, "").Current.State)
	require.JSONEq(t, "[]", get("/posts/"+capture.PostUUID+"/publishers", 200).Body.String())
	var history []publisherDecision
	path := "/captures/" + capture.UUID + "/publisher-history"
	require.NoError(t, json.Unmarshal(get(path+"?limit=1", 200).Body.Bytes(), &history))
	require.Len(t, history, 1)
	require.Equal(t, receipt, history[0])
	require.NoError(t, json.Unmarshal(get(fmt.Sprintf("%s?after=%d", path, receipt.Revision), 200).Body.Bytes(), &history))
	require.Len(t, history, 1)
	require.Equal(t, "unlinked", history[0].State)
	databasePath := db.DatabasePath()
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(databasePath))
	require.NoError(t, json.Unmarshal(get("/capture-publisher/requests/"+input.RequestUUID, 200).Body.Bytes(), &receipt))
	require.Equal(t, first.Decision, receipt, "the original acknowledgement survives reopen")
	require.Equal(t, "unlinked", preview(capture.UUID, "").Current.State)
}

func TestCapturePublisherHTTPRejectsConflictsAndInvalidRequests(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	capture := publisherHTTPCapture(t, repo)
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	send := publisherHTTPSend(t, handler)
	for _, account := range []*models.SourceAccount{
		publisherHTTPAccount(t, repo, "native:twitter", "id", "123"),
		publisherHTTPAccount(t, repo, "native:reddit", "id", "t2_other"),
	} {
		var preview publisherPreview
		require.NoError(t, json.Unmarshal(send("/capture-publisher/preview", map[string]string{
			"capture_uuid": capture.UUID, "account_uuid": account.UUID}, 200).Body.Bytes(), &preview))
		require.NotEmpty(t, preview.Conflicts)
		send("/capture-publisher/apply", publisherApplyInput{RequestUUID: uuid.NewString(), CaptureUUID: capture.UUID,
			Signature: preview.Signature, Action: "link", AccountUUID: account.UUID}, 409)
	}
	for _, body := range []any{
		map[string]string{"capture_uuid": "123"},
		map[string]string{"capture_uuid": capture.UUID, "account_uuid": "123"},
		map[string]any{"capture_uuid": capture.UUID, "unknown": true},
	} {
		send("/capture-publisher/preview", body, 400)
	}
	for _, input := range []publisherApplyInput{
		{RequestUUID: uuid.NewString(), CaptureUUID: capture.UUID, Signature: strings.Repeat("g", 64), Action: "unlink"},
		{RequestUUID: uuid.NewString(), CaptureUUID: capture.UUID, Signature: strings.Repeat("a", 64), Action: "link"},
		{RequestUUID: uuid.NewString(), CaptureUUID: capture.UUID, Signature: strings.Repeat("a", 64), Action: "migration"},
		{RequestUUID: uuid.NewString(), CaptureUUID: capture.UUID, Signature: strings.Repeat("a", 64), Action: "automatic", AccountUUID: uuid.NewString()},
		{RequestUUID: uuid.NewString(), CaptureUUID: capture.UUID, Signature: strings.Repeat("a", 64), Action: "unlink", Reason: "bad\x00reason"},
	} {
		send("/capture-publisher/apply", input, 400)
	}
	send("/capture-publisher/preview", map[string]string{"capture_uuid": uuid.NewString()}, 404)
	get("/capture-publisher/requests/"+uuid.NewString(), 404)
	get("/capture-publisher/requests/bad", 400)
	get("/captures/"+uuid.NewString()+"/publisher-history", 404)
	get("/captures/bad/publisher-history", 400)
	for _, query := range []string{"after=-1", "after=bad", "limit=0", "limit=101"} {
		get("/captures/"+capture.UUID+"/publisher-history?"+query, 400)
	}
	for _, path := range []string{"/capture-publisher/preview", "/capture-publisher/apply"} {
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{}`))
		r.Header.Set("Origin", "https://unrelated.example")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusForbidden, w.Code)
		producerHandler := withIngestRoutes(http.NotFoundHandler(), ingest.New(repo), false)
		w = httptest.NewRecorder()
		producerHandler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ingestPath+path, bytes.NewBufferString(`{}`)))
		require.Equal(t, http.StatusNotFound, w.Code)
	}
	require.JSONEq(t, "[]", get("/captures/"+capture.UUID+"/publisher-history", 200).Body.String())
}
