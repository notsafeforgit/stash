package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSourceTrackingSavedSettingRequiresRestart(t *testing.T) {
	cfg := config.NewEmpty()
	cfg.SetConfigFile(filepath.Join(t.TempDir(), "config.yml"))
	p := paths.NewPaths(t.TempDir(), t.TempDir())
	settings := &settingsService{config: cfg, runtime: &manager.Manager{Config: cfg, Paths: &p}}
	for _, active := range []bool{false, true} {
		// Starting another server captures the saved setting as its capability.
		require.Equal(t, active, cfg.GetSourceTrackingEnabled())
		r := &Resolver{settings: settings, sourceTrackingActive: cfg.GetSourceTrackingEnabled()}
		server := handler.New(newLibrarySchema(Config{Resolvers: r}))
		server.AddTransport(transport.POST{})
		body, err := json.Marshal(map[string]any{
			"query":     `mutation($enabled: Boolean!) { configureGeneral(input:{sourceTrackingEnabled:$enabled}) { sourceTrackingEnabled sourceTrackingActive } }`,
			"variables": map[string]bool{"enabled": !active},
		})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		expected, err := json.Marshal(map[string]any{"data": map[string]any{"configureGeneral": map[string]bool{"sourceTrackingEnabled": !active, "sourceTrackingActive": active}}})
		require.NoError(t, err)
		require.JSONEq(t, string(expected), w.Body.String())
		require.Equal(t, !active, cfg.GetSourceTrackingEnabled())
	}
}

func TestSourceTrackingDisabledDoesNotConstructWorkers(t *testing.T) {
	cfg := config.InitializeEmpty()
	// A stale translation setting cannot independently enable source workers.
	cfg.SetBool(config.TranslationWorkerEnabled, true)
	server := &Server{manager: &manager.Manager{Config: cfg}}
	service, routes := server.configureSourceTracking(cfg.GetSourceTrackingEnabled())
	require.Nil(t, service)
	require.False(t, routes.fileIngestion)
	for _, worker := range []*backgroundWorkerRuntime{
		server.ingestWorker, server.albumWorker, server.postMergeNotifications,
		server.translationWorker, server.enrichmentMaintenance,
		server.discoveryMaintenance, server.discoveryComparison, server.discoveryPublication,
	} {
		require.Nil(t, worker)
		worker.start()
		worker.stop()
	}
	// Producer paths remain JSON errors; they must not fall through to the SPA
	// or acknowledge events while there is no worker to consume them.
	handler := withIngestRoutes(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), service, false)
	for _, path := range []string{ingestPath, ingestPath + "/capabilities", ingestPath + "/batches", ingestPath + "/runs"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{}`)))
		require.Equal(t, http.StatusServiceUnavailable, w.Code)
		require.Contains(t, w.Body.String(), "source_tracking_disabled")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/scenes", nil))
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestSourceTrackingDisabledPreservesLibraryAndExistingSources(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "library.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	routes := &nativeArchiveRoutes{repo: db.Repository()}
	enabled, disabled := routes.routerWithSourceTracking(true), routes.routerWithSourceTracking(false)
	request := func(handler http.Handler, method, path string, input any) *httptest.ResponseRecorder {
		body, err := json.Marshal(input)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	// Ordinary library folders remain fully manageable with tracking off.
	rootID := "10000000-0000-4000-8000-000000000001"
	w := request(disabled, http.MethodPost, "/media-roots/probe", map[string]string{"server_path": t.TempDir()})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var binding models.MediaRootBinding
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &binding))
	w = request(disabled, http.MethodPut, "/media-roots/"+rootID, map[string]any{"label": "Local library", "state": "active", "binding": binding})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	for _, path := range []string{"/media-roots", "/media-roots/" + rootID, "/page-info?resource=media-roots", "/page-info?resource=root-history&subject=" + rootID, "/page-info?resource=shares", "/metadata-fields/scene"} {
		w = request(disabled, http.MethodGet, path, nil)
		require.Equal(t, http.StatusOK, w.Code, path+": "+w.Body.String())
	}
	// Create a source, then check disabling/re-enabling never rewrites it.
	collectionID := "20000000-0000-4000-8000-000000000001"
	input := models.SourceCollectionInput{SourceCollectionDefinition: models.SourceCollectionDefinition{RootUUID: &rootID, Kind: "directory", Label: "Existing imports", State: "active", PathPrefix: "creator"}, Reason: "Fixture"}
	w = request(enabled, http.MethodPut, "/collections/"+collectionID, input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	before := w.Body.String()
	for _, path := range []string{"/collections", "/posts", "/source-accounts", "/activity/jobs", "/manual-intake/capabilities", "/page-info?resource=posts", "/page-info?resource=accounts"} {
		w = request(disabled, http.MethodGet, path, nil)
		require.Equal(t, http.StatusServiceUnavailable, w.Code, path)
	}
	w = request(disabled, http.MethodPut, "/collections/"+collectionID, input)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	w = request(enabled, http.MethodGet, "/collections/"+collectionID, nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, before, w.Body.String())
}
