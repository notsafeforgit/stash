package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestManualIntakeHTTPPreviewAdmissionAndSavedRecovery(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "manual-http.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "Purchased.mp4")
	require.NoError(t, os.WriteFile(filePath, []byte("not yet verified media"), 0600))
	binding, err := archive.ProbeMediaRoot(dir)
	require.NoError(t, err)
	var collection *models.SourceCollection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		root, err := repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Files", State: "active", Binding: binding}})
		if err != nil {
			return err
		}
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Purchased", Kind: "directory", State: "active", RootUUID: &root.UUID, PathPrefix: "."}})
		return err
	}))
	routes := &nativeArchiveRoutes{repo: repo, fileIngestion: true}
	handler := routes.router()
	request := func(method, target string, body any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, target, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	input := ingest.ManualFileInput{CollectionUUID: collection.UUID, RelativePath: "Purchased.mp4", MediaKind: models.ArchiveScene}
	w := request(http.MethodGet, "/manual-intake/capabilities", nil)
	require.JSONEq(t, `{"file_ingestion":true}`, w.Body.String())
	w = request(http.MethodPost, "/manual-intake/preview", input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.NotContains(t, w.Body.String(), dir)
	require.NotContains(t, w.Body.String(), "change_token")
	var preview ingest.ManualFilePreview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	require.Equal(t, "Purchased.mp4", preview.Filename)
	require.Empty(t, preview.ExistingFileUUID)
	apply := ingest.ManualFileRequest{ManualFileInput: input, RequestUUID: uuid.NewString(), Signature: preview.Signature}
	stale := apply
	stale.Signature = ingest.Digest([]byte("different"))
	w = request(http.MethodPost, "/manual-intake/apply", stale)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "intake_preview_changed")
	w = request(http.MethodPost, "/manual-intake/apply", apply)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var accepted ingest.ManualFileStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &accepted))
	require.False(t, accepted.RegistrationCommitted)
	require.False(t, accepted.MediaIngested)
	require.NotContains(t, w.Body.String(), dir)
	require.NoError(t, os.Remove(filePath))
	routes.fileIngestion = false
	w = request(http.MethodPost, "/manual-intake/apply", apply)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var recovered ingest.ManualFileStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recovered))
	require.Equal(t, accepted, recovered, "lost response recovery works while the file and worker are unavailable")
	w = request(http.MethodGet, "/manual-intake/requests/"+apply.RequestUUID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recovered))
	require.Equal(t, accepted, recovered)
	w = request(http.MethodPost, "/manual-intake/requests/"+apply.RequestUUID+"/cancel", map[string]int64{"expected_revision": accepted.Revision + 1})
	require.Equal(t, http.StatusConflict, w.Code)
	w = request(http.MethodPost, "/manual-intake/requests/"+apply.RequestUUID+"/cancel", map[string]int64{"expected_revision": accepted.Revision})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recovered))
	require.Equal(t, "cancelled", recovered.State)
	w = request(http.MethodGet, "/manual-intake/requests/"+apply.RequestUUID, nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"state":"cancelled"`)
	apply.Signature = stale.Signature
	w = request(http.MethodPost, "/manual-intake/apply", apply)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "intake_request_changed")
	apply.RequestUUID = uuid.NewString()
	w = request(http.MethodPost, "/manual-intake/apply", apply)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "file_ingestion_unavailable")
	require.Equal(t, http.StatusNotFound, request(http.MethodGet, "/manual-intake/requests/"+apply.RequestUUID, nil).Code)
	require.Equal(t, http.StatusBadRequest, request(http.MethodGet, "/manual-intake/requests/not-a-uuid", nil).Code)
	for _, raw := range []string{
		`{"collection_uuid":"` + collection.UUID + `","relative_path":"Purchased.mp4","media_kind":"scene","producer_uuid":"` + uuid.NewString() + `"}`,
		`{"collection_uuid":"` + collection.UUID + `","relative_path":"Purchased.mp4","relative_path":"other.mp4","media_kind":"scene"}`,
	} {
		r := httptest.NewRequest(http.MethodPost, "/manual-intake/preview", bytes.NewBufferString(raw))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
	r := httptest.NewRequest(http.MethodPost, "/manual-intake/preview", bytes.NewBufferString(`{}`))
	r.Header.Set("Origin", "https://other.example")
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	require.Equal(t, http.StatusForbidden, w.Code)
	// These routes remain behind the existing application-authenticated mount.
	// The producer router cannot admit manual application work on that prefix.
	privateCalls := 0
	outer := withIngestRoutes(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		privateCalls++
		w.WriteHeader(http.StatusUnauthorized)
	}), ingest.New(repo), true)
	r = httptest.NewRequest(http.MethodPost, "/api/v3/archive/manual-intake/apply", bytes.NewBufferString(`{}`))
	r.Header.Set("Authorization", "Bearer ingest_producer_token")
	w = httptest.NewRecorder()
	outer.ServeHTTP(w, r)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, 1, privateCalls)
}
