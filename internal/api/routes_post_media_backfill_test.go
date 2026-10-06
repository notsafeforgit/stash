package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func postMediaBackfillHTTPFixture(t *testing.T) (*sqlite.Database, models.Repository, string) {
	t.Helper()
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "post-media-matching.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var postUUID string
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO folders(id,path,basename,mod_time,created_at,updated_at) VALUES(1,'/post-match','post-match',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at) VALUES(1,1,'clip.mp4',100,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO video_files(file_id,duration,video_codec,format,audio_codec,width,height,frame_rate,bit_rate) VALUES(1,1,'h264','mp4','aac',64,48,30,100);
INSERT INTO scenes(id,title,created_at,updated_at) VALUES(1,'Curated',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(1,1,1);`, nil)
		if err != nil {
			return err
		}
		media, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveScene, 1)
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
		collection, err := repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Retained catalog", Kind: "legacy_catalog", State: "disabled"}})
		if err != nil {
			return err
		}
		observation, err := repo.SourceFile.RecordObservation(ctx, models.SourceFileObservation{UUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
			RootUUID: root.UUID, RootRevision: root.Revision, RelativePath: "clip.mp4", State: "present", Role: "local", Origin: "migration", ObservedAt: time.Now()})
		if err != nil {
			return err
		}
		match, err := repo.SourceFile.RecordMatch(ctx, models.SourceFileMatch{UUID: uuid.NewString(), ObservationUUID: observation.UUID, FileUUID: file.UUID,
			Generation: 1, LibraryRootPath: "/post-match", Basis: "exact-path", Origin: "migration"})
		if err != nil {
			return err
		}
		post, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "old-post"}, "")
		if err != nil {
			return err
		}
		postUUID = post.UUID
		postFile, err := repo.SourceFile.RecordPostEvidence(ctx, models.SourcePostFileEvidence{SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: postUUID,
			Origin: "migration", Basis: "catalog-appearance", ObservedAt: time.Now()}, ObservationUUID: observation.UUID})
		if err != nil {
			return err
		}
		details, err := json.Marshal(map[string]string{"source_post_file_evidence_uuid": postFile.UUID, "source_file_match_uuid": match.UUID})
		if err != nil {
			return err
		}
		_, err = repo.SourceAttachment.RecordMediaEvidence(ctx, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: postUUID, MediaUUID: media.UUID,
			FileUUID: &file.UUID, Basis: "legacy", Details: details})
		return err
	}))
	return db, repo, postUUID
}

func TestPostMediaBackfillHTTPPreviewApplyRecoveryAndScope(t *testing.T) {
	db, repo, post := postMediaBackfillHTTPFixture(t)
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	request := func(method, path string, input any) *httptest.ResponseRecorder {
		body, err := json.Marshal(input)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := request(http.MethodGet, "/post-media-backfill-posts?limit=1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var posts []string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &posts))
	require.Equal(t, []string{post}, posts)
	require.JSONEq(t, `[]`, request(http.MethodGet, "/post-media-backfill-posts?after="+post, nil).Body.String())
	for _, query := range []string{"after=no", "limit=0", "limit=101", "limit=1.5"} {
		response := request(http.MethodGet, "/post-media-backfill-posts?"+query, nil)
		require.Equal(t, http.StatusBadRequest, response.Code, query+": "+response.Body.String())
	}
	base := "/posts/" + post
	w = request(http.MethodGet, base+"/media-backfill-preview", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var preview models.SourcePostMediaMatchPreview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	require.Equal(t, models.SourcePostMediaCatalogFilesV1, preview.Policy)
	require.Len(t, preview.Candidates, 1)
	require.Equal(t, "matched", preview.Candidates[0].Status)
	input := models.SourcePostMediaBackfillInput{UUID: uuid.NewString(), PostUUID: post, Signature: preview.Signature}
	w = request(http.MethodPost, base+"/media-backfills", input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	saved := append([]byte{}, w.Body.Bytes()...)
	var result models.SourcePostMediaBackfillResult
	require.NoError(t, json.Unmarshal(saved, &result))
	require.Equal(t, 1, result.Selected)
	require.Len(t, result.Decisions, 1)
	require.Equal(t, saved, request(http.MethodPost, base+"/media-backfills", input).Body.Bytes())
	require.Equal(t, saved, request(http.MethodGet, "/post-media-backfills/"+input.UUID, nil).Body.Bytes())
	w = request(http.MethodGet, "/post-media-decisions/"+result.Decisions[0].UUID+"/evidence", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var evidence []models.SourcePostMediaMatchedEvidence
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &evidence))
	require.Len(t, evidence, 1)
	require.Equal(t, preview.Candidates[0].Proofs[0].EvidenceUUID, evidence[0].EvidenceUUID)
	stale := input
	stale.UUID = uuid.NewString()
	require.Equal(t, http.StatusConflict, request(http.MethodPost, base+"/media-backfills", stale).Code)
	stale.PostUUID = uuid.NewString()
	require.Equal(t, http.StatusBadRequest, request(http.MethodPost, base+"/media-backfills", stale).Code)
	require.Equal(t, http.StatusNotFound, request(http.MethodGet, "/post-media-backfills/"+uuid.NewString(), nil).Code)
	csrf := httptest.NewRequest(http.MethodPost, base+"/media-backfills", bytes.NewReader([]byte(`{}`)))
	csrf.Header.Set("Origin", "https://unrelated.example")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, csrf)
	require.Equal(t, http.StatusForbidden, w.Code)
	var producer *models.IngestProducer
	var collection *models.SourceCollection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		producer, err = repo.Ingest.CreateProducer(ctx, "Scoped scraper")
		if err != nil {
			return err
		}
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Feed", Kind: "feed", State: "active"}})
		return err
	}))
	intake := ingest.New(repo)
	_, token, err := intake.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID}}, nil)
	require.NoError(t, err)
	producerHandler := withIngestRoutes(http.NotFoundHandler(), intake, false)
	for _, path := range []string{ingestPath + base + "/media-backfills", "/api/v3/archive" + base + "/media-backfills"} {
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{}`)))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		producerHandler.ServeHTTP(w, r)
		require.Equal(t, http.StatusNotFound, w.Code)
	}
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	handler = (&nativeArchiveRoutes{repo: db.Repository()}).router()
	require.Equal(t, saved, request(http.MethodPost, base+"/media-backfills", input).Body.Bytes())
}
