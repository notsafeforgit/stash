package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

func nativeMetadataReviewFixture(t *testing.T) (*sqlite.Database, models.Repository, models.MetadataFileEditInput) {
	t.Helper()
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "metadata-review.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var input models.MetadataFileEditInput
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
		file, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveFile, 1)
		if err != nil {
			return err
		}
		root, err := repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "migration", MediaRootDefinition: models.MediaRootDefinition{Label: "Old root", State: "disabled"}})
		if err != nil {
			return err
		}
		collection, err := repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Old catalog", Kind: "legacy_catalog", State: "disabled"}})
		if err != nil {
			return err
		}
		observation, err := repo.SourceFile.RecordObservation(ctx, models.SourceFileObservation{UUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
			RootUUID: root.UUID, RootRevision: root.Revision, RelativePath: "clip.mp4", State: "present", Role: "local", Origin: "migration", ObservedAt: time.Now()})
		if err != nil {
			return err
		}
		match, err := repo.SourceFile.RecordMatch(ctx, models.SourceFileMatch{UUID: uuid.NewString(), ObservationUUID: observation.UUID, FileUUID: file.UUID,
			Generation: 1, LibraryRootPath: "/review", Basis: "exact-path", Origin: "migration"})
		if err != nil {
			return err
		}
		edits, err := archive.CatalogFileEdits([]byte(`{"title":"Historical title"}`))
		if err != nil {
			return err
		}
		history, err := repo.SourceFileHistory.Record(ctx, models.SourceFileHistory{UUID: uuid.NewString(), Kind: "metadata_edit", CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
			RootUUID: root.UUID, RootRevision: root.Revision, ReferenceNamespace: "fixture:catalog", ReferenceValue: "edit", Origin: "migration", ObservedAt: time.Now(), Edits: edits,
			Locations: []models.SourceFileHistoryLocation{{RelativePath: "clip.mp4", ObservationUUID: &observation.UUID}}})
		if err != nil {
			return err
		}
		input = models.MetadataFileEditInput{EntityUUID: entity.UUID, HistoryUUID: history.UUID, SourceField: "title", MatchUUID: match.UUID}
		return nil
	}))
	return db, repo, input
}

func TestNativeMetadataReviewHTTPSelectionRetryAndAfterCommit(t *testing.T) {
	_, repo, input := nativeMetadataReviewFixture(t)
	notifications, failHook := 0, false
	handler := (&nativeArchiveRoutes{repo: repo, notifyMetadata: func(ctx context.Context, in metadata.Input, fields []string) error {
		require.Equal(t, input.EntityUUID, in.EntityUUID)
		require.Equal(t, []string{"title"}, fields)
		txn.AddPostCommitHook(ctx, func(context.Context) { notifications++ })
		if failHook {
			return errors.New("notification registration failed")
		}
		return nil
	}}).router()
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		return result
	}
	w := request(http.MethodGet, "/entity-identities/scene/1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var identity metadataEntityIdentity
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &identity))
	require.Equal(t, input.EntityUUID, identity.UUID)
	w = request(http.MethodGet, "/entities/"+identity.UUID+"/file-edits?limit=1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var alternatives []models.MetadataFileEditCandidate
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &alternatives))
	require.Len(t, alternatives, 1)
	require.Equal(t, input.HistoryUUID, alternatives[0].HistoryUUID)
	w = request(http.MethodGet, "/entities/"+identity.UUID+"/file-edits?after_history="+input.HistoryUUID+"&after_match="+input.MatchUUID+"&limit=1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, "[]", w.Body.String())
	w = request(http.MethodPost, "/metadata-file-edits/preview", input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var preview models.MetadataFileEditPreview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	require.Equal(t, "ready", preview.Status)
	require.Equal(t, `"Curated"`, string(preview.CurrentValue))
	require.Zero(t, notifications)
	apply := models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
	for retry := 0; retry < 2; retry++ {
		w = request(http.MethodPost, "/metadata-file-edits/apply", apply)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var result struct {
			Review   *models.MetadataFileEditReview `json:"review"`
			Replayed bool                           `json:"replayed"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Equal(t, retry == 1, result.Replayed)
		require.Equal(t, apply.RequestUUID, result.Review.RequestUUID)
		require.Equal(t, 1, notifications)
	}
	w = request(http.MethodGet, "/metadata-file-edits/requests/"+apply.RequestUUID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = request(http.MethodGet, "/entities/"+identity.UUID+"/metadata-fields", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"file_edit"`)
	require.NotContains(t, w.Body.String(), `"settings"`)
	var fields struct {
		Fields []struct {
			Decision map[string]json.RawMessage `json:"decision"`
		} `json:"fields"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fields))
	for _, field := range fields.Fields {
		require.NotContains(t, field.Decision, "value", "a current-field response must not duplicate its selected value under provenance")
	}
	w = request(http.MethodGet, "/entities/"+identity.UUID+"/metadata-fields/title/history?limit=1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var history []models.MetadataFieldDecision
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &history))
	require.Len(t, history, 1)
	require.Equal(t, "preserved", history[0].Mode)
	apply.SourceField = "details"
	w = request(http.MethodPost, "/metadata-file-edits/apply", apply)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	apply.MetadataFileEditInput = input
	apply.RequestUUID = uuid.NewString()
	w = request(http.MethodPost, "/metadata-file-edits/apply", apply)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	// Failing hook registration rolls back both the choice and its retry receipt.
	w = request(http.MethodPost, "/metadata-file-edits/preview", input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	apply.Digest, failHook = preview.Digest, true
	w = request(http.MethodPost, "/metadata-file-edits/apply", apply)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Equal(t, 1, notifications)
	w = request(http.MethodGet, "/metadata-file-edits/requests/"+apply.RequestUUID, nil)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	csrf := httptest.NewRequest(http.MethodPost, "/metadata-file-edits/apply", bytes.NewReader([]byte(`{}`)))
	csrf.Header.Set("Origin", "https://another.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, csrf)
	require.Equal(t, http.StatusForbidden, response.Code)
}
