package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeBackupCheckpointHTTPAndPortableExport(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python 3 is required for the portable archive interoperability check")
	}
	cfg := config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	directory := t.TempDir()
	cfg.SetConfigFile(filepath.Join(directory, "config.yml"))
	cfg.SetString(config.Database, filepath.Join(directory, "library.sqlite"))
	cfg.SetString(config.BackupDirectoryPath, directory)
	cfg.SetString(config.ApiKey, "http-backup-fixture")
	cfg.SetUIConfiguration(map[string]interface{}{"theme": "dark"})
	require.NoError(t, cfg.Write())
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(cfg.GetDatabasePath()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	mgr := &manager.Manager{Config: cfg, Database: db, Repository: db.Repository()}
	router := chi.NewRouter()
	router.Mount("/api/v3/backups", (&nativeBackupRoutes{manager: mgr}).router())
	// Exercise the real handlers/client with an application-key gate. The
	// production mount uses the existing authenticateHandler, not this fixture.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "http-backup-fixture" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		router.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	media := filepath.Join(directory, "media & <escaped> \u2028")
	require.NoError(t, os.Mkdir(media, 0700))
	roots, err := json.Marshal([]manager.NativeCheckpointRoot{{Name: "media", Path: media}})
	require.NoError(t, err)
	rootsFile, keyFile := filepath.Join(directory, "roots.json"), filepath.Join(directory, "api-key")
	require.NoError(t, os.WriteFile(rootsFile, roots, 0600))
	require.NoError(t, os.WriteFile(keyFile, []byte("http-backup-fixture\n"), 0600))
	id := uuid.NewString()
	archive, err := filepath.Abs("../../integrations/archive")
	require.NoError(t, err)
	source := filepath.Join(archive, "src")
	entries, err := os.ReadDir(filepath.Join(source, "stash_archive"))
	require.NoError(t, err)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".py") {
			_, err := os.ReadFile(filepath.Join(source, "stash_archive", entry.Name()))
			require.NoError(t, err)
		}
	}
	script := filepath.Join(archive, "tests", "server_checkpoint_check.py")
	_, err = os.ReadFile(script)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]string{"server": server.URL, "directory": directory,
		"request_id": id, "roots_file": rootsFile, "key_file": keyFile})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, script)
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+source)
	command.Stdin = bytes.NewReader(body)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	var result struct {
		Verified bool   `json:"verified"`
		Restored string `json:"restored"`
	}
	require.NoError(t, json.Unmarshal(output, &result))
	require.True(t, result.Verified)
	require.Equal(t, filepath.Join(directory, "restored"), result.Restored)
	proof, err := sqlite.VerifyNativeSnapshot(t.Context(), filepath.Join(result.Restored, "library.sqlite"))
	require.NoError(t, err)
	require.Zero(t, proof.PendingFileDeletions)
	_, err = file.RestoreDeletionSnapshot(t.Context(), filepath.Join(result.Restored, "components", "file_journal", "deletions.zip"),
		filepath.Join(directory, "rebound"), nil, nil)
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(directory, "native-checkpoints", id, "library.sqlite"))
	require.FileExists(t, filepath.Join(directory, "native-checkpoints", id, "release.json"))
	for _, check := range []struct {
		path, key, origin string
		status            int
	}{
		{"/api/v3/backups/checkpoints/" + id, "", "", http.StatusUnauthorized},
		{"/api/v3/backups/checkpoints/" + id, "producer-token", "", http.StatusUnauthorized},
		{"/api/v3/backups/checkpoints/" + id, "http-backup-fixture", "https://foreign.example", http.StatusForbidden},
		{"/api/v3/backups/checkpoints/" + id, "http-backup-fixture", "", http.StatusGone},
		{"/api/v3/backups/checkpoints/" + id + "/components/library.sqlite", "http-backup-fixture", "", http.StatusGone},
		{"/api/v3/backups/checkpoints/" + id + "/release", "http-backup-fixture", "", http.StatusOK},
		{"/api/v3/backups/checkpoints/" + id + "/components/not-in-inventory", "http-backup-fixture", "", http.StatusBadRequest},
	} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+check.path, nil)
		require.NoError(t, err)
		req.Header.Set("ApiKey", check.key)
		req.Header.Set("Origin", check.origin)
		response, err := server.Client().Do(req)
		require.NoError(t, err)
		require.Equal(t, check.status, response.StatusCode)
		require.NoError(t, response.Body.Close())
	}
}
