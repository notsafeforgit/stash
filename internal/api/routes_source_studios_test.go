package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceStudioHTTPAssignmentAndOptionalIntegration(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	var c *models.SourceCollection
	var studioUUID string
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		c, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: uuid.NewString(), Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Aggregate", Kind: "subreddit", State: "active"}})
		if err != nil {
			return err
		}
		s := models.NewStudio()
		s.Name = "Subreddit"
		if err := repo.Studio.Create(ctx, &models.CreateStudioInput{Studio: &s}); err != nil {
			return err
		}
		e, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveStudio, s.ID)
		if err == nil {
			studioUUID = e.UUID
		}
		return err
	}))
	path := "/collections/" + c.UUID + "/studio"
	var current models.SourceStudioAssignment
	require.NoError(t, json.Unmarshal(get(path, 200).Body.Bytes(), &current))
	require.Zero(t, current.Revision)
	require.Nil(t, current.Studio)
	routes := (&nativeArchiveRoutes{repo: repo})
	put := func(handler http.Handler, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		return w
	}
	body := `{"expected_revision":0,"studio_uuid":"` + studioUUID + `"}`
	put(routes.routerWithSourceTracking(false), body, 503)
	put(routes.router(), `{"expected_revision":0,"studio_uuid":"bad"}`, 400)
	put(routes.router(), body, 200)
	put(routes.router(), body, 200)
	put(routes.router(), `{"expected_revision":0,"studio_uuid":null}`, 409)
	require.NoError(t, json.Unmarshal(get(path, 200).Body.Bytes(), &current))
	require.Equal(t, studioUUID, current.Studio.UUID)
	put(routes.router(), `{"expected_revision":1,"studio_uuid":null}`, 200)
	get("/posts/"+uuid.NewString()+"/studios", 404)
}
