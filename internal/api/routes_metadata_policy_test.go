package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeMetadataHTTPPreviewApplyAndSchema(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "policy.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	notified := 0
	routes := &nativeArchiveRoutes{repo: repo, notifyMetadata: func(ctx context.Context, input metadata.Input, fields []string) error {
		notified++
		require.Equal(t, []string{"title"}, fields)
		state, err := repo.MetadataField.State(ctx, input.EntityUUID, "title")
		require.Equal(t, `"Purchased clip"`, string(state.Value))
		return err
	}}
	handler := routes.router()
	request := func(method, target string, body interface{}) *httptest.ResponseRecorder {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, target, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	dir := t.TempDir()
	w := request(http.MethodPost, "/media-roots", map[string]interface{}{"label": "Manual", "state": "active", "server_path": dir})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var root models.MediaRoot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &root))
	w = request(http.MethodPost, "/collections", models.SourceCollectionInput{SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Purchased", Kind: "directory", State: "active", RootUUID: &root.UUID, PathPrefix: "."}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var collection models.SourceCollection
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &collection))
	w = request(http.MethodGet, "/collections/"+collection.UUID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var found models.SourceCollection
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &found))
	require.Equal(t, collection, found)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, http.StatusBadRequest, request(http.MethodGet, "/collections/not-a-uuid", nil).Code)
	require.Equal(t, http.StatusNotFound, request(http.MethodGet, "/collections/00000000-0000-4000-8000-000000000001", nil).Code)
	var entity, file *models.ArchiveEntity
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO folders(id,path,basename,mod_time,created_at,updated_at) VALUES(1,?,'manual',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, []interface{}{dir})
		if err != nil {
			return err
		}
		_, _, err = db.ExecSQL(ctx, `INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at) VALUES(1,1,'Purchased clip.mp4',100,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO video_files(file_id,duration,video_codec,format,audio_codec,width,height,frame_rate,bit_rate) VALUES(1,1,'h264','mp4','aac',64,48,30,100);
INSERT INTO scenes(id,created_at,updated_at) VALUES(1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(1,1,1);`, nil)
		if err != nil {
			return err
		}
		entity, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveScene, 1)
		if err != nil {
			return err
		}
		file, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveFile, 1)
		return err
	}))
	put := models.MetadataPolicyInput{ExpectedCollectionRevision: collection.Revision, Definition: models.MetadataPolicyDefinition{Enabled: true, ApplyToScans: true, Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{models.ArchiveScene: {OnCreate: true, OnExisting: true, FilenameTitleFallback: true}}}}
	policyPath := "/collections/" + collection.UUID + "/metadata-policy"
	w = request(http.MethodPut, policyPath, put)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = request(http.MethodGet, "/metadata-fields/scene", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var fields []models.MetadataFieldDefinition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fields))
	for _, field := range fields {
		require.NotContains(t, []string{"id", "files", "fingerprints"}, field.Name)
	}
	previewRequest := metadataPreviewRequest{CollectionUUID: collection.UUID, EntityUUID: entity.UUID, FileUUID: file.UUID, IncludeData: true}
	w = request(http.MethodPost, "/metadata-policy/preview", previewRequest)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var preview metadata.Preview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	require.False(t, preview.Input.Created)
	require.NotContains(t, preview.Data, "settings")
	require.Zero(t, notified)
	previewRequest.Digest = preview.Digest
	w = request(http.MethodPost, "/metadata-policy/apply", previewRequest)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 1, notified)
	w = request(http.MethodPost, "/metadata-policy/apply", previewRequest)
	require.Equal(t, http.StatusConflict, w.Code)
	require.Equal(t, 1, notified)
	// Unknown fields cannot fabricate a creation event or pass a local path.
	w = request(http.MethodPost, "/metadata-policy/preview", map[string]interface{}{"created": true, "path": "/arbitrary"})
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = request(http.MethodPut, policyPath, put)
	require.Equal(t, http.StatusConflict, w.Code, "policy PUT requires the current revision")
	csrf := httptest.NewRequest(http.MethodPost, "/metadata-policy/apply", bytes.NewReader([]byte(`{}`)))
	csrf.Header.Set("Origin", "https://another.example")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, csrf)
	require.Equal(t, http.StatusForbidden, w.Code)
}
