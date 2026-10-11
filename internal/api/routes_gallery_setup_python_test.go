package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// Exercise the public setup command against the actual HTTP routes and SQLite,
// including its default metadata policy and subsequent queue-only registration.
// No source websites or production services are involved.
func TestGallerySetupConnectsLibraryAndRegistersDownload(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	mediaPath := filepath.Join(directory, "media")
	require.NoError(t, os.Mkdir(mediaPath, 0700))
	binding, err := archive.ProbeMediaRoot(mediaPath)
	require.NoError(t, err)
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "library.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	service := ingest.New(db.Repository())
	var root *models.MediaRoot
	require.NoError(t, service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		root, err = service.Repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{
			Label: "Downloaded media", State: "active", Binding: binding,
		}})
		return err
	}))
	routes := &ingestRoutes{service: service, fileIngestion: true, runs: ingest.NewRunCoordinator(service)}
	app := chi.NewRouter()
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/v3/ingest/") && r.Header.Get("ApiKey") != "fixture-application-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	app.Mount("/api/v3/ingest-admin", routes.adminRouter())
	app.Mount("/api/v3/archive", (&nativeArchiveRoutes{repo: service.Repo, fileIngestion: true}).router())
	producer := routes.router()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v3/ingest/") {
			producer.ServeHTTP(w, r)
			return
		}
		app.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	workerState := filepath.Join(directory, "worker")
	command := func(arguments ...string) map[string]interface{} {
		t.Helper()
		args := append([]string{"-m", "stash_gallery", "--directory", workerState}, arguments...)
		cmd := exec.CommandContext(t.Context(), python, args...)
		cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "PYTHONDONTWRITEBYTECODE=1", "STASH_API_KEY=fixture-application-key")
		body, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", body)
		var value map[string]interface{}
		require.NoError(t, json.Unmarshal(body, &value), "%s", body)
		return value
	}
	first := command("setup", "--endpoint", server.URL, "--root", root.UUID, "--media-path", mediaPath)
	require.Equal(t, "ready", first["state"])
	second := command("setup", "--endpoint", server.URL, "--root", root.UUID, "--media-path", mediaPath)
	require.Equal(t, first, second)
	require.Equal(t, "ready", command("check")["state"])
	require.Equal(t, "idle", command("worker")["state"], "the generated dispatcher works without starting a website request")
	queued := command("download", "--queue-only", "https://www.reddit.com/user/example/")
	require.Equal(t, "recorded", queued["state"])
	resumed := command("resume", queued["call_uuid"].(string), "--queue-only")
	require.Equal(t, queued, resumed, "resume keeps the same durable call and target registration")
}
