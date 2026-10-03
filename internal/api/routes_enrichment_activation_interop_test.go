package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonEnrichmentActivationRecoversLostResponseAndKeepsReviewedPlan(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "native.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	source, snapshot := uuid.NewString(), uuid.NewString()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if _, _, err := db.ExecSQL(ctx, "INSERT INTO performers(id,created_at,updated_at) VALUES(71,'2020-01-01 00:00:00','2020-01-01 00:00:00'); INSERT INTO performer_names(performer_id,name,position) VALUES(71,'Preserved name',0)", nil); err != nil {
			return err
		}
		for i := range 205 {
			ref, err := scrape.CatalogLocalPostReference(source, "c_"+strings.Repeat("1", 32), fmt.Sprintf("reddit:post:activation%03d", i))
			if err != nil {
				return err
			}
			if _, err := repo.SourceEvidence.EnsurePost(ctx, ref, ""); err != nil {
				return err
			}
		}
		return nil
	}))
	handler := http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: repo}).router())
	var lost atomic.Bool
	var applies atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		if recorder.Code == 200 && r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/enrichment-activations") {
			applies.Add(1)
			if !lost.Swap(true) {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = connection.Close()
				return
			}
		}
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	defer server.Close()
	setup, err := json.Marshal(map[string]any{"directory": directory, "source": source, "snapshot": snapshot, "endpoint": server.URL})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_enrichment_activation.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, lost.Load())
	require.EqualValues(t, 3, applies.Load(), "replay inspects existing receipts before sending new Apply requests")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		progress, err := repo.AutomationEnrichmentImport.Find(ctx, snapshot)
		require.NoError(t, err)
		for after, count := int64(0), 0; ; {
			rows, err := repo.AutomationEnrichmentImport.HeldTargets(ctx, snapshot, progress.ManifestSHA256, after, 100)
			require.NoError(t, err)
			for _, row := range rows {
				require.Equal(t, "changed", row.Disposition)
				require.Equal(t, "excluded", row.State)
				require.Equal(t, 2, row.CurrentRevision)
				require.Equal(t, 25, row.Priority)
				destination, err := repo.EnrichmentWork.Target(ctx, row.ReleasedTargetUUID)
				require.NoError(t, err)
				require.Equal(t, "pending", destination.State)
				require.Equal(t, 2, destination.CollectionRevision)
				require.Equal(t, row.Priority, destination.Priority)
				require.True(t, row.NotBefore.Equal(destination.NotBefore))
				count++
				after = row.Ordinal
			}
			if len(rows) < 100 {
				require.Equal(t, 205, count)
				break
			}
		}
		for _, state := range []string{"queued", "running", "succeeded", "failed", "cancelled"} {
			jobs, err := repo.ArchiveJob.List(ctx, models.ArchiveJobEnrichPost, state, 0, 100)
			require.NoError(t, err)
			require.Empty(t, jobs, "activation never creates metadata collector jobs")
		}
		return nil
	}))
}
