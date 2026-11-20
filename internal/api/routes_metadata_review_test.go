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
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// nativeMetadataFixture opens a database holding one scene with one file and
// returns the scene's archive entity UUID.
func nativeMetadataFixture(t *testing.T) (*sqlite.Database, models.Repository, string) {
	t.Helper()
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "metadata-review.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var scene string
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO folders(id,path,basename,mod_time,created_at,updated_at) VALUES(1,'/review','review',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at) VALUES(1,1,'clip.mp4',100,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO video_files(file_id,duration,video_codec,format,audio_codec,width,height,frame_rate,bit_rate) VALUES(1,1,'h264','mp4','aac',64,48,30,100);
 INSERT INTO scenes(id,title,created_at,updated_at) VALUES(1,'Curated',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(1,1,1);`, nil)
		if err != nil {
			return err
		}
		entity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveScene, 1)
		if err != nil {
			return err
		}
		scene = entity.UUID
		return nil
	}))
	return db, repo, scene
}

func TestNativeMetadataFieldsHTTPIdentityProvenanceAndHistory(t *testing.T) {
	_, repo, scene := nativeMetadataFixture(t)
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	request := func(method, path string) *httptest.ResponseRecorder {
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, httptest.NewRequest(method, path, nil))
		return result
	}
	w := request(http.MethodGet, "/entity-identities/scene/1")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var identity metadataEntityIdentity
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &identity))
	require.Equal(t, scene, identity.UUID)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		entity, err := repo.ArchiveEntity.Find(ctx, scene)
		if err != nil {
			return err
		}
		_, err = repo.MetadataField.Decide(ctx, models.MetadataFieldDecisionInput{EntityUUID: scene, ExpectedEntityRevision: entity.Revision,
			Field: "title", Mode: "set", Value: json.RawMessage(`"Reviewed title"`), Origin: "review", Reason: "Owner choice"})
		return err
	}))
	w = request(http.MethodGet, "/entities/"+identity.UUID+"/metadata-fields")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), `"settings"`)
	require.NotContains(t, w.Body.String(), `"file_edit"`)
	var fields struct {
		Fields []struct {
			Definition models.MetadataFieldDefinition `json:"definition"`
			Value      json.RawMessage                `json:"value"`
			Decision   map[string]json.RawMessage     `json:"decision"`
		} `json:"fields"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fields))
	reviewed := false
	for _, field := range fields.Fields {
		require.NotContains(t, field.Decision, "value", "a current-field response must not duplicate its selected value under provenance")
		if field.Definition.Name == "title" {
			reviewed = true
			require.JSONEq(t, `"Reviewed title"`, string(field.Value))
			require.JSONEq(t, `"Owner choice"`, string(field.Decision["reason"]))
		}
	}
	require.True(t, reviewed)
	w = request(http.MethodGet, "/entities/"+identity.UUID+"/metadata-fields/title/history?limit=1")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var history []models.MetadataFieldDecision
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &history))
	require.Len(t, history, 1)
	require.Equal(t, "set", history[0].Mode)
	// The historical file-edit routes are retired with their evidence.
	for _, path := range []string{"/entities/" + identity.UUID + "/file-edits", "/metadata-file-edits/requests/" + scene, "/file-history/" + scene} {
		require.Equal(t, http.StatusNotFound, request(http.MethodGet, path).Code, path)
	}
	csrf := httptest.NewRequest(http.MethodPost, "/file-deduplication/preview", bytes.NewReader([]byte(`{}`)))
	csrf.Header.Set("Origin", "https://another.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, csrf)
	require.Equal(t, http.StatusForbidden, response.Code)
}
