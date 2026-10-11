package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSourceSubscriptionHTTPScopesAndApplicationControl(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "subscriptions.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	service := ingest.New(db.Repository())
	var root *models.MediaRoot
	var collections []*models.SourceCollection
	var producer *models.IngestProducer
	require.NoError(t, service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		root, err = service.Repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Library", State: "active"}})
		require.NoError(t, err)
		for _, name := range []string{"first", "second"} {
			c, err := service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
				Label: name, Kind: "account", State: "active", Namespace: "native:reddit", RootUUID: &root.UUID, PathPrefix: ".", TargetURL: "https://www.reddit.com/user/" + name + "/"}})
			require.NoError(t, err)
			collections = append(collections, c)
			_, err = service.Repo.SourceSubscription.Put(ctx, c.UUID, models.SourceSubscriptionInput{Enabled: true})
			require.NoError(t, err)
		}
		producer, err = service.Repo.Ingest.CreateProducer(ctx, "Limited worker")
		return err
	}))
	_, token, err := service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collections[0].UUID, RootUUID: &root.UUID}}, nil)
	require.NoError(t, err)
	worker := (&ingestRoutes{service: service}).router()
	read := func(credential string) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]string{"root_uuid": root.UUID, "namespace": "native:reddit", "after": ""})
		require.NoError(t, err)
		r := httptest.NewRequest(http.MethodPost, ingestPath+"/subscriptions/list", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+credential)
		w := httptest.NewRecorder()
		worker.ServeHTTP(w, r)
		return w
	}
	w := read(token)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var page models.SourceSubscriptionPage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Equal(t, 1, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, collections[0].UUID, page.Items[0].Collection.UUID)
	require.Equal(t, http.StatusUnauthorized, read("bad").Code)
	r := httptest.NewRequest(http.MethodPut, "/collections/"+collections[0].UUID+"/subscription", bytes.NewBufferString(`{"expected_revision":1,"enabled":false}`))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	(&nativeArchiveRoutes{repo: service.Repo}).router().ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = read(token)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Zero(t, page.Total)
}
