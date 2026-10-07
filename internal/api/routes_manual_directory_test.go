package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestManualDirectoryHTTPScopesFoldersAndChecksContinuations(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "picker.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "purchases"), 0700))
	for _, name := range []string{"a.mp4", "b.jpg", "sound.mp3", "unfinished.mp4.part"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "purchases", name), []byte(name), 0600))
	}
	binding, err := archive.ProbeMediaRoot(dir)
	require.NoError(t, err)
	var collection *models.SourceCollection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		root, err := repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Local media", State: "active", Binding: binding}})
		if err != nil {
			return err
		}
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Purchases", Kind: "directory", State: "active", RootUUID: &root.UUID, PathPrefix: "purchases"}})
		return err
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	get := func(query string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/collections/"+collection.UUID+"/intake-files"+query, nil))
		return w
	}
	w := get("?limit=1")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.NotContains(t, w.Body.String(), dir)
	var page ingest.ManualDirectoryPage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Equal(t, "purchases", page.Directory)
	require.Equal(t, "purchases/a.mp4", page.Entries[0].RelativePath)
	require.Equal(t, "scene", page.Entries[0].Kind)
	w = get("?limit=1&after=" + page.NextAfter + "&signature=" + page.Signature)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	page = ingest.ManualDirectoryPage{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Equal(t, "purchases/b.jpg", page.Entries[0].RelativePath)
	require.Empty(t, page.NextAfter)
	for _, query := range []string{"?directory=.", "?directory=../purchases", "?limit=101", "?limit=0", "?limit=1&limit=2", "?other=value", "?after=1/a.mp4", "?q=%00"} {
		require.Equal(t, http.StatusBadRequest, get(query).Code, query)
	}
	w = get("?q=.jpg")
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "a.mp4")
	for _, query := range []string{"?directory=purchases", "?directory=.", "?directory=purchases&directory=purchases", "?directory=purchases&unknown=true"} {
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/collections/"+collection.UUID+"/scan-scope"+query, nil))
		if query != "?directory=purchases" {
			require.Equal(t, http.StatusBadRequest, w.Code)
			continue
		}
		require.Equal(t, http.StatusOK, w.Code)
		require.NotContains(t, w.Body.String(), dir)
		var scope ingest.ManualScanScope
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &scope))
		require.Equal(t, collection.UUID, scope.ScanCollectionUUID)
		require.Equal(t, collection.UUID, scope.CollectionUUID)
	}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		definition := collection.SourceCollectionDefinition
		definition.State = "disabled"
		_, err := repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	require.Equal(t, http.StatusConflict, get("").Code)
}
