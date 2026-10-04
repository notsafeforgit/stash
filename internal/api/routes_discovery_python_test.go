package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestPythonDiscoveryHTTPPreservesLargePagesAndLostAcknowledgements(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	f := newDiscoveryHTTPFixtureAt(t, time.Now().UTC().Truncate(time.Millisecond))
	worker := ingest.NewDiscoveryCoordinator(f.service)
	f.handler = (&ingestRoutes{service: f.service, discovery: worker}).router()
	var mu sync.Mutex
	lost := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, suffix := range []string{"/jobs", "/claim", "/page", "/failure"} {
			if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, suffix) {
				continue
			}
			committed := httptest.NewRecorder()
			f.handler.ServeHTTP(committed, r)
			mu.Lock()
			lose := committed.Code == 200 && !lost[suffix]
			if lose {
				lost[suffix] = true
			}
			mu.Unlock()
			if lose {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(503)
				_, _ = io.WriteString(w, `{"error":"temporarily_unavailable"}`)
				return
			}
			for key, values := range committed.Header() {
				w.Header()[key] = values
			}
			w.WriteHeader(committed.Code)
			_, _ = w.Write(committed.Body.Bytes())
			return
		}
		f.handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	setup, err := json.Marshal(map[string]any{"endpoint": server.URL, "producer": f.producer.UUID, "listing": f.listing})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests/http_discovery_interop.py"))
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_INGEST_TOKEN="+f.token)
	command.Stdin = bytes.NewReader(setup)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	require.NoError(t, command.Run(), stderr.String())
	var result struct {
		Receipt   models.DiscoveryPageReceipt `json:"receipt"`
		FailedJob string                      `json:"failed_job"`
		PageBytes int                         `json:"page_bytes"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.Greater(t, result.PageBytes, 4<<20)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		page, err := f.repo.DiscoveryJob.Page(ctx, f.listing.UUID, 1)
		require.NoError(t, err)
		require.Equal(t, result.Receipt, page.DiscoveryPageReceipt)
		require.Equal(t, result.PageBytes, len(page.Body))
		for _, token := range []string{`1.2300e+05`, `0.123456789012345678901234567890`, `"zero":-0`, `\u2028`, `\u2029`} {
			require.Contains(t, string(page.Body), token)
		}
		for id, state := range map[string]string{page.JobUUID: "succeeded", result.FailedJob: "failed"} {
			job, err := f.repo.ArchiveJob.Find(ctx, id)
			require.NoError(t, err)
			require.Equal(t, state, job.State)
			attempts, err := f.repo.ArchiveJob.Attempts(ctx, id, 0, 10)
			require.NoError(t, err)
			require.Len(t, attempts, 1, "a lost acknowledgement cannot create another attempt")
		}
		pages, err := f.repo.DiscoveryJob.Pages(ctx, f.listing.UUID, 0, 10)
		require.NoError(t, err)
		require.Len(t, pages, 1)
		require.False(t, pages[0].Complete, "one retained page is not a completed listing")
		return nil
	}))
	mu.Lock()
	require.Len(t, lost, 4)
	mu.Unlock()
}
