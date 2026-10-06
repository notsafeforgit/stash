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

func TestManualIntakeHTTPRetryRecoveryAndUnavailableWorker(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "retry.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample.mp4"), []byte("queued bytes"), 0600))
	binding, err := archive.ProbeMediaRoot(dir)
	require.NoError(t, err)
	repo := db.Repository()
	var collection *models.SourceCollection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		root, err := repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Files", State: "active", Binding: binding}})
		if err != nil {
			return err
		}
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Files", Kind: "directory", State: "active", RootUUID: &root.UUID, PathPrefix: "."}})
		return err
	}))
	service := ingest.New(repo)
	input := ingest.ManualFileInput{CollectionUUID: collection.UUID, RelativePath: "sample.mp4", MediaKind: models.ArchiveScene}
	preview, err := service.PreviewManualFile(t.Context(), input)
	require.NoError(t, err)
	original := uuid.NewString()
	admitted, err := service.SubmitManualFile(t.Context(), ingest.ManualFileRequest{ManualFileInput: input, RequestUUID: original, Signature: preview.Signature})
	require.NoError(t, err)
	cancelled, err := service.CancelManualFile(t.Context(), original, admitted.Revision)
	require.NoError(t, err)
	routes := &nativeArchiveRoutes{repo: repo, fileIngestion: true}
	handler := routes.router()
	id := uuid.NewString()
	call := func(request string, revision int64) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]any{"request_uuid": request, "expected_revision": revision})
		require.NoError(t, err)
		r := httptest.NewRequest(http.MethodPost, "/manual-intake/requests/"+original+"/retry", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := call(id, cancelled.Revision)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var first ingest.ManualFileStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))
	require.Equal(t, id, first.RequestUUID)
	require.Equal(t, cancelled.JobUUID, first.ResumeFromJobUUID)
	routes.fileIngestion = false
	require.NoError(t, os.Remove(filepath.Join(dir, "sample.mp4")))
	w = call(id, cancelled.Revision)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var recovered ingest.ManualFileStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recovered))
	require.Equal(t, first, recovered)
	require.Equal(t, http.StatusConflict, call(id, cancelled.Revision+1).Code)
	require.Equal(t, http.StatusServiceUnavailable, call(uuid.NewString(), cancelled.Revision).Code)
	require.Equal(t, http.StatusBadRequest, call(id, 0).Code)
}
