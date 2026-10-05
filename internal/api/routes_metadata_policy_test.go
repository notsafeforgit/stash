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
	w := request(http.MethodPost, "/media-roots/probe", map[string]interface{}{"server_path": dir})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var binding models.MediaRootBinding
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &binding))
	w = request(http.MethodPost, "/media-roots", map[string]interface{}{"label": "Manual", "state": "active", "binding": binding})
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
	var entity, file, performer *models.ArchiveEntity
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO folders(id,path,basename,mod_time,created_at,updated_at) VALUES(1,?,'manual',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, []interface{}{dir})
		if err != nil {
			return err
		}
		_, _, err = db.ExecSQL(ctx, `INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at) VALUES(1,1,'Purchased clip.mp4',100,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO video_files(file_id,duration,video_codec,format,audio_codec,width,height,frame_rate,bit_rate) VALUES(1,1,'h264','mp4','aac',64,48,30,100);
INSERT INTO scenes(id,created_at,updated_at) VALUES(1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO performers(id,created_at,updated_at) VALUES(100,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO performer_names(performer_id,name,position) VALUES(100,'Selected performer',0);
INSERT INTO studios(id,name,created_at,updated_at) VALUES(100,'Canonical studio',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO studio_aliases(studio_id,alias) VALUES(100,'Studio alias');
INSERT INTO tags(id,name,created_at,updated_at) VALUES(100,'Canonical tag',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO tag_aliases(tag_id,alias) VALUES(100,'Tag alias');
INSERT INTO groups(id,name,created_at,updated_at) VALUES(100,'Source album',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(1,1,1);`, nil)
		if err != nil {
			return err
		}
		entity, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveScene, 1)
		if err != nil {
			return err
		}
		file, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveFile, 1)
		if err != nil {
			return err
		}
		performer, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, 100)
		return err
	}))
	w = request(http.MethodPost, "/metadata-policy/references", map[string]interface{}{"uuids": []string{performer.UUID, entity.UUID, "00000000-0000-4000-8000-000000000001"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var references []metadataPolicyReference
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &references))
	require.Len(t, references, 3)
	require.Equal(t, performer.UUID, references[0].RequestedUUID)
	require.Equal(t, "Selected performer", references[0].Name)
	require.NotNil(t, references[0].Entity)
	require.Nil(t, references[1].Entity, "scenes are not supported constant relationships")
	require.Nil(t, references[2].Entity, "keep missing references visible without creating them")
	require.Equal(t, http.StatusBadRequest, request(http.MethodPost, "/metadata-policy/references", map[string]interface{}{"uuids": []string{"bad"}}).Code)
	put := models.MetadataPolicyInput{ExpectedCollectionRevision: collection.Revision, Definition: models.MetadataPolicyDefinition{Enabled: true, ApplyToScans: true, Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{models.ArchiveScene: {OnCreate: true, OnExisting: true, FilenameTitleFallback: true}}}}
	policyPath := "/collections/" + collection.UUID + "/metadata-policy"
	draftRequest := metadataPolicyDraftRequest{CollectionUUID: collection.UUID, ExpectedCollectionRevision: collection.Revision,
		EntityUUID: entity.UUID, FileUUID: file.UUID, Definition: put.Definition, Event: "create", IncludeData: true}
	w = request(http.MethodPost, "/metadata-policy/draft-preview", draftRequest)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var draft map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &draft))
	require.NotContains(t, draft, "digest")
	require.Equal(t, true, draft["context"].(map[string]interface{})["created"])
	require.JSONEq(t, "null", request(http.MethodGet, policyPath, nil).Body.String(), "draft leaves the policy absent")
	require.Zero(t, notified)
	nameDraft := draftRequest
	nameDraft.Definition = models.MetadataPolicyDefinition{Enabled: true, Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{
		models.ArchiveScene: {OnCreate: true, Mappings: map[string]models.MetadataMapping{
			"studio": {Value: json.RawMessage(`"STUDIO ALIAS"`), ReferenceNames: true},
			"tags":   {Value: json.RawMessage(`["Tag alias"]`), ReferenceNames: true},
			"groups": {Value: json.RawMessage(`[{"name":"source album","scene_index":2}]`), ReferenceNames: true},
		}},
	}}
	w = request(http.MethodPost, "/metadata-policy/draft-preview", nameDraft)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var namePreview metadata.DraftPreview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &namePreview))
	require.Len(t, namePreview.Changes, 3)
	for _, change := range namePreview.Changes {
		require.Equal(t, "ready", change.Status)
		require.Equal(t, "matched", change.Names[0].Status)
	}
	require.JSONEq(t, "null", request(http.MethodGet, policyPath, nil).Body.String(), "name previews cannot save a policy")
	require.Zero(t, notified)
	nameDraft.Definition.Rules[models.ArchiveScene].Mappings["title"] = models.MetadataMapping{Value: json.RawMessage(`"Invalid name target"`), ReferenceNames: true}
	require.Equal(t, http.StatusBadRequest, request(http.MethodPost, "/metadata-policy/draft-preview", nameDraft).Code)
	w = request(http.MethodPost, "/metadata-policy/apply", draftRequest)
	require.Equal(t, http.StatusBadRequest, w.Code, "Apply cannot accept a draft definition or simulated creation")
	draftRequest.ExpectedCollectionRevision++
	require.Equal(t, http.StatusConflict, request(http.MethodPost, "/metadata-policy/draft-preview", draftRequest).Code)
	draftRequest.ExpectedCollectionRevision--
	draftRequest.Event = "scan"
	require.Equal(t, http.StatusBadRequest, request(http.MethodPost, "/metadata-policy/draft-preview", draftRequest).Code)
	draftRequest.Event = "existing"
	draftRequest.IncludeData = false
	w = request(http.MethodPost, "/metadata-policy/draft-preview", draftRequest)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	draft = nil
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &draft))
	require.NotContains(t, draft, "data")
	samplePath := policyPath + "/samples/" + entity.UUID
	w = request(http.MethodGet, samplePath+"/files?collection_revision=1&limit=1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var sampleFiles []models.MetadataPolicySampleFile
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &sampleFiles))
	require.Equal(t, []models.MetadataPolicySampleFile{{FileUUID: file.UUID, RelativePath: "Purchased clip.mp4"}}, sampleFiles)
	w = request(http.MethodGet, samplePath+"/sources?collection_revision=1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, `[]`, w.Body.String(), "manual files need no invented source")
	for _, target := range []string{samplePath + "/files", samplePath + "/files?collection_revision=1&limit=0", samplePath + "/sources?collection_revision=1&after_capture=" + file.UUID} {
		require.Equal(t, http.StatusBadRequest, request(http.MethodGet, target, nil).Code)
	}
	w = request(http.MethodPut, policyPath, put)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, http.StatusConflict, request(http.MethodPost, "/metadata-policy/draft-preview", draftRequest).Code, "the saved policy revision changed")
	require.Equal(t, http.StatusBadRequest, request(http.MethodGet, policyPath+"/history?limit=0", nil).Code)
	w = request(http.MethodGet, policyPath+"/history?limit=1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var policies []models.MetadataPolicy
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &policies))
	require.Len(t, policies, 1)
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
