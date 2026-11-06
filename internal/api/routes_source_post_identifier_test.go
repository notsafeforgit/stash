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
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestReviewedSourcePostIdentifierHTTP(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "identifier-review.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var post, other *models.SourcePost
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "album"}, "")
		if err != nil {
			return err
		}
		other, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:instagram", Value: "456"}, "")
		return err
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	input := models.SourcePostIdentifierInput{
		SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post.UUID,
			Origin: "review", Basis: "verified_album_identity", ObservedAt: time.Now().UTC(),
			Details: json.RawMessage(`{"evidence":"saved filenames and captured album IDs"}`)},
		Identifier:           models.SourcePostIdentifier{Namespace: "native:instagram", Value: "123"},
		ExpectedPostRevision: post.Revision,
	}
	path := "/posts/" + post.UUID + "/identifiers"
	apply := func(value models.SourcePostIdentifierInput, status int) *httptest.ResponseRecorder {
		body, err := json.Marshal(value)
		require.NoError(t, err)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(response, request)
		require.Equal(t, status, response.Code, response.Body.String())
		return response
	}
	invalid := input
	invalid.Origin = "capture"
	apply(invalid, http.StatusBadRequest)
	invalid = input
	invalid.PostUUID = other.UUID
	apply(invalid, http.StatusBadRequest)
	invalid = input
	invalid.ExpectedPostRevision++
	apply(invalid, http.StatusConflict)
	invalid = input
	invalid.Identifier.Value = "456"
	apply(invalid, http.StatusConflict)
	invalid = input
	invalid.Basis = ""
	apply(invalid, http.StatusBadRequest)

	first := apply(input, http.StatusOK)
	var evidence models.SourcePostIdentifierObservation
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &evidence))
	require.Equal(t, input.UUID, evidence.UUID)
	require.Equal(t, input.Identifier, evidence.Identifier)
	require.Equal(t, "review", evidence.Origin)
	var committedRevision int
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.SourceEvidence.FindPost(ctx, post.UUID)
		if err == nil {
			committedRevision = current.Revision
		}
		return err
	}))
	require.Greater(t, committedRevision, post.Revision)
	require.JSONEq(t, first.Body.String(), apply(input, http.StatusOK).Body.String())
	changed := input
	changed.Identifier.Value = "789"
	apply(changed, http.StatusConflict)
	stale := input
	stale.UUID, stale.Identifier.Value = uuid.NewString(), "789"
	apply(stale, http.StatusConflict)

	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.SourceEvidence.FindPost(ctx, post.UUID)
		require.NoError(t, err)
		require.Equal(t, committedRevision, current.Revision)
		matched, err := repo.SourceEvidence.FindPostByIdentifier(ctx, input.Identifier)
		require.NoError(t, err)
		require.Equal(t, post.UUID, matched.UUID)
		identifiers, err := repo.SourceEvidence.PostIdentifiers(ctx, post.UUID, nil, 10)
		require.NoError(t, err)
		require.Len(t, identifiers, 2)
		return nil
	}))
}
