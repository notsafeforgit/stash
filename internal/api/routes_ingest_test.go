package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestIngestHTTPAuthenticationPartialBatchAndReceiptIsolation(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "ingest.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	service := ingest.New(db.Repository())
	routes := &ingestRoutes{service: service}
	var producer *models.IngestProducer
	var collection *models.SourceCollection
	require.NoError(t, service.Repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		producer, err = service.Repo.Ingest.CreateProducer(ctx, "HTTP worker")
		require.NoError(t, err)
		collection, err = service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Twitter feed", Kind: "feed", Namespace: "native:twitter", State: "active"}, Origin: "review"})
		return err
	}))
	credential, token, err := service.IssueCredential(context.Background(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID}}, nil)
	require.NoError(t, err)
	privateCalls := 0
	handler := withIngestRoutes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { privateCalls++; w.WriteHeader(http.StatusTeapot) }), service, false)
	request := func(method, target, auth string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		r.AddCookie(&http.Cookie{Name: "session", Value: "owner-cookie"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	capabilities := ingestPath + "/capabilities"
	require.Equal(t, http.StatusUnauthorized, request(http.MethodGet, capabilities, "", nil).Code)
	require.Equal(t, http.StatusUnauthorized, request(http.MethodGet, capabilities, "Bearer ordinary-api-key", nil).Code)
	require.Equal(t, http.StatusBadRequest, request(http.MethodGet, capabilities+"?token="+token, "", nil).Code)
	good := request(http.MethodGet, capabilities, "Bearer "+token, nil)
	require.Equal(t, http.StatusOK, good.Code)
	require.Contains(t, good.Body.String(), `"file_ingestion":false`)
	var supported struct {
		Namespaces []string `json:"post_namespaces"`
		Prefixes   []string `json:"post_namespace_prefixes"`
	}
	require.NoError(t, json.Unmarshal(good.Body.Bytes(), &supported))
	require.Contains(t, supported.Namespaces, "native:bluesky")
	require.NotContains(t, supported.Namespaces, "native:onlyfans", "mirror evidence does not advertise native OnlyFans support")
	require.Contains(t, supported.Prefixes, "mirror:coomer:")
	require.Contains(t, supported.Prefixes, "mirror:kemono:")
	require.NotContains(t, good.Body.String(), credential.SecretHash)
	require.Equal(t, 0, privateCalls)
	for _, target := range []string{ingestPath + "/graphql", ingestPath + "/../graphql", ingestPath + "/batches/", ingestPath + "/%63apabilities"} {
		w := request(http.MethodGet, target, "Bearer "+token, nil)
		require.Contains(t, []int{http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed}, w.Code, target)
	}
	require.Equal(t, 0, privateCalls)
	require.Equal(t, http.StatusTeapot, request(http.MethodGet, "/api/v3/ingest-admin/producers", "Bearer "+token, nil).Code)
	require.Equal(t, 1, privateCalls, "admin routes retain application authentication")
	event := ingest.CaptureEvent{Protocol: 1, ProducerUUID: producer.UUID, EventUUID: uuid.NewString(), RunUUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, Kind: "source.capture", ObservedAt: time.Now().UTC(), ExtractorVersion: "fixture", RetentionPolicy: archive.SourceRetentionVersion,
		Post: ingest.PostReference{Namespace: "native:twitter", Value: "9007199254740993"}, Source: []byte(`{"category":"twitter","tweet_id":9007199254740993,"author":{"id":"99","name":"Example"},"extended_entities":{"media":[{"id_str":"101","type":"photo"},{"id_str":"102","type":"video"}]}}`)}
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	item := fmt.Sprintf(`{"sha256":%q,"event":%s}`, ingest.Digest(raw), raw)
	// The middle item fails independently; both acknowledged items identify the
	// same committed receipt and do not create another capture or album.
	body := []byte(`{"events":[` + item + `,{"sha256":"bad","event":{}},` + item + `]}`)
	w := request(http.MethodPost, ingestPath+"/batches", "Bearer "+token, body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var response struct {
		Results []ingestBatchResult `json:"results"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Results, 3)
	require.Equal(t, http.StatusOK, response.Results[0].Status)
	require.Equal(t, http.StatusBadRequest, response.Results[1].Status)
	require.Nil(t, response.Results[1].Receipt)
	require.Equal(t, response.Results[0].Receipt, response.Results[2].Receipt)
	w = request(http.MethodGet, ingestPath+"/receipts/"+event.EventUUID, "Bearer "+token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var receipt models.IngestReceipt
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &receipt))
	require.Equal(t, *response.Results[0].Receipt, receipt)
	// A digest is over the exact durable event bytes, including whitespace.
	changed := fmt.Sprintf(`{"events":[{"sha256":%q,"event": %s }]}`, ingest.Digest(append([]byte{' '}, raw...)), raw)
	w = request(http.MethodPost, ingestPath+"/batches", "Bearer "+token, []byte(changed))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"invalid_event"`)
	for _, bad := range []string{`{"events":[]}`, `{"events":[],"unknown":true}`, `{"events":[],"events":[]}`} {
		require.Equal(t, http.StatusBadRequest, request(http.MethodPost, ingestPath+"/batches", "Bearer "+token, []byte(bad)).Code)
	}
	require.NoError(t, service.Repo.WithTxn(context.Background(), func(ctx context.Context) error { return service.Repo.Ingest.RevokeCredential(ctx, credential.UUID) }))
	require.Equal(t, http.StatusUnauthorized, request(http.MethodGet, ingestPath+"/receipts/"+event.EventUUID, "Bearer "+token, nil).Code)
	// Administration issues a secret once and never includes its verifier in
	// credential listing. Cross-site requests cannot issue or revoke tokens.
	admin := chi.NewRouter()
	admin.Mount("/api/v3/ingest-admin", routes.adminRouter())
	adminRequest := func(method, target, origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		admin.ServeHTTP(w, r)
		return w
	}
	issue := fmt.Sprintf(`{"scopes":[{"collection_uuid":%q,"root_uuid":null}]}`, collection.UUID)
	target := "/api/v3/ingest-admin/producers/" + producer.UUID + "/credentials"
	require.Equal(t, http.StatusForbidden, adminRequest(http.MethodPost, target, "https://elsewhere.test", issue).Code)
	w = adminRequest(http.MethodPost, target, "", issue)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"token":"ingest_`)
	w = adminRequest(http.MethodGet, target, "", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "secret_hash")
	require.NotContains(t, w.Body.String(), `"token"`)
}
