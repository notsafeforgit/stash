package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPostMediaHTTPReviewReplayRestartAndValidation(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "post-media.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var post *models.SourcePost
	var media *models.ArchiveEntity
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if _, _, err := db.ExecSQL(ctx, `INSERT INTO scenes(id,created_at,updated_at) VALUES(1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, nil); err != nil {
			return err
		}
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "old-post"}, "")
		if err != nil {
			return err
		}
		media, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveScene, 1)
		return err
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	path := "/posts/" + post.UUID + "/media/" + media.UUID
	w := request(http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var association models.SourcePostMediaAssociation
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &association))
	require.Equal(t, "undecided", association.State)
	input := models.SourcePostMediaInput{UUID: uuid.NewString(), PostUUID: post.UUID, MediaUUID: media.UUID, ExpectedPostRevision: association.PostRevision, ExpectedMediaRevision: association.MediaRevision, State: "linked", Origin: "review", Reason: "Reviewed retained metadata"}
	w = request(http.MethodPut, path, input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	saved := append([]byte{}, w.Body.Bytes()...)
	require.Equal(t, saved, request(http.MethodPut, path, input).Body.Bytes(), "lost response reuses the original decision")
	require.Equal(t, saved, request(http.MethodGet, "/post-media-decisions/"+input.UUID, nil).Body.Bytes())
	stale := input
	stale.UUID = uuid.NewString()
	stale.State = "unlinked"
	require.Equal(t, http.StatusConflict, request(http.MethodPut, path, stale).Code)
	stale.UUID = input.UUID
	require.Equal(t, http.StatusConflict, request(http.MethodPut, path, stale).Code)
	invalid := input
	invalid.Origin = "migration"
	require.Equal(t, http.StatusBadRequest, request(http.MethodPut, path, invalid).Code)
	require.Equal(t, http.StatusBadRequest, request(http.MethodGet, "/posts/bad/media/"+media.UUID, nil).Code)
	require.Equal(t, http.StatusBadRequest, request(http.MethodGet, path+"/history?limit=1000", nil).Code)
	w = request(http.MethodGet, path+"/history?limit=1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var history []models.SourcePostMediaDecision
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &history))
	require.Len(t, history, 1)
	require.Equal(t, input.UUID, history[0].UUID)
	r := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(saved))
	r.Header.Set("Origin", "https://other.example")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	handler = (&nativeArchiveRoutes{repo: db.Repository()}).router()
	w = request(http.MethodPut, path, input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, saved, w.Body.Bytes())
}
