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

func TestProfileAccountHTTPBindingReplayAndOptOut(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "profile.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var collection *models.SourceCollection
	var owner *models.ArchiveEntity
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		p := models.NewPerformer()
		p.Name = "Profile owner"
		p.URLs = models.NewRelatedStrings([]string{"https://www.redgifs.com/users/example"})
		require.NoError(t, repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &p}))
		var err error
		owner, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, p.ID)
		require.NoError(t, err)
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Redgifs", Kind: "account", Namespace: "native:redgifs", State: "active", TargetURL: "https://www.redgifs.com/users/example",
		}})
		return err
	}))
	path := "/collections/" + collection.UUID + "/profile-account"
	routes := &nativeArchiveRoutes{repo: repo}
	request := func(handler http.Handler, body any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	input := models.ProfileAccountLink{ExpectedRevision: collection.Revision, PerformerUUID: owner.UUID}
	w := request(routes.routerWithSourceTracking(false), input)
	require.NotEqual(t, http.StatusOK, w.Code, "installation opt-in still applies")
	w = request(routes.router(), input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var result models.ProfileAccountLinkResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "linked", result.State)
	require.NotNil(t, result.Collection.AccountUUID)
	replay := request(routes.router(), input)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.JSONEq(t, w.Body.String(), replay.Body.String())
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		account, err := repo.SourceAccount.Find(ctx, *result.Collection.AccountUUID)
		require.NoError(t, err)
		_, err = repo.SourceAccount.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: account.UUID,
			ExpectedAccountRevision: account.Revision, State: models.AccountOwnershipUnlinked, Origin: "review"})
		return err
	}))
	w = request(routes.router(), input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "review", result.State)
	require.Equal(t, "ownership_choice", result.Reason)
}
