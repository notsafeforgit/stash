package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestPythonDiscoveryDispatchResumesWithoutRepeatingPages(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	script := filepath.Join(packagePath, "tests/http_discovery_dispatch.py")
	invoke := func(t *testing.T, setup map[string]any, token string, private bool) []byte {
		t.Helper()
		body, err := json.Marshal(setup)
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, python, script)
		command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_INGEST_TOKEN="+token)
		if private {
			command.Env = append(command.Env, "DISCOVERY_FIXTURE_LOGIN=fixture-private-site-token")
		}
		command.Stdin = bytes.NewReader(body)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		require.NoError(t, command.Run(), stderr.String())
		require.NotContains(t, stdout.String(), "fixture-private-site-token")
		require.NotContains(t, stderr.String(), "fixture-private-site-token")
		return stdout.Bytes()
	}
	var policy struct {
		Digest string `json:"policy_sha256"`
	}
	require.NoError(t, json.Unmarshal(invoke(t, map[string]any{"directory": t.TempDir(), "policy_only": true}, "", true), &policy))
	fixture, err := filepath.Abs("../../pkg/archive/testdata/discovery-pages-v1.json")
	require.NoError(t, err)
	for _, scenario := range []string{"lost_admission", "lost_page", "filtered_page"} {
		t.Run(scenario, func(t *testing.T) {
			f := newDiscoveryHTTPFixtureForPolicy(t, time.Date(2026, 10, 4, 14, 0, 0, 0, time.UTC), policy.Digest)
			worker := ingest.NewDiscoveryCoordinator(f.service)
			var now atomic.Int64
			now.Store(f.now.UnixMilli())
			worker.Now = func() time.Time { return time.UnixMilli(now.Load()).UTC() }
			f.handler = (&ingestRoutes{service: f.service, discovery: worker}).router()
			if scenario == "filtered_page" {
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					for i := range 25 {
						input := f.listing.DiscoveryListingInput
						input.UUID, input.PolicySHA256 = fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1), strings.Repeat("b", 64)
						if _, err := f.repo.DiscoveryJob.CreateListing(ctx, input, worker.Now()); err != nil {
							return err
						}
					}
					return nil
				}))
			}
			var lost atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Date", worker.Now().Format(http.TimeFormat))
				suffix := "/jobs"
				if scenario == "lost_page" {
					suffix = "/page"
				}
				if scenario != "filtered_page" && r.Method == "POST" && strings.HasSuffix(r.URL.Path, suffix) && lost.CompareAndSwap(false, true) {
					committed := httptest.NewRecorder()
					f.handler.ServeHTTP(committed, r)
					require.Equal(t, 200, committed.Code, committed.Body.String())
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(503)
					_, _ = io.WriteString(w, `{"error":"temporarily_unavailable"}`)
					return
				}
				f.handler.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			directory := t.TempDir()
			type dispatchResult struct {
				Result struct {
					State   string                       `json:"state"`
					Receipt *models.DiscoveryPageReceipt `json:"receipt"`
				} `json:"result"`
				Fetches int `json:"fetches"`
			}
			run := func(cycle int, delivery bool) dispatchResult {
				t.Helper()
				var result dispatchResult
				raw := invoke(t, map[string]any{"endpoint": server.URL, "producer": f.producer.UUID, "collection": f.collection.UUID,
					"directory": directory, "fixture": fixture, "clock": 1000 + cycle*60, "delivery_only": delivery}, f.token, !delivery)
				require.NoError(t, json.Unmarshal(raw, &result))
				return result
			}
			first := run(0, false)
			switch scenario {
			case "lost_admission":
				require.Equal(t, "unavailable", first.Result.State)
				require.Zero(t, first.Fetches)
			case "lost_page":
				require.Equal(t, "delivery_pending", first.Result.State)
				require.Equal(t, 1, first.Fetches)
			case "filtered_page":
				require.Equal(t, "waiting", first.Result.State, "an empty filtered page is not the end of readiness traversal")
				require.Zero(t, first.Fetches)
			}
			completed := false
			for cycle := 1; cycle <= 8; cycle++ {
				now.Add(int64(2 * time.Minute / time.Millisecond))
				result := run(cycle, scenario == "lost_page" && cycle == 1)
				require.Contains(t, []string{"waiting", "idle", "page_delivered"}, result.Result.State)
				if scenario == "lost_page" && cycle == 1 {
					require.Equal(t, "page_delivered", result.Result.State)
					require.Equal(t, 1, result.Fetches, "delivery-only CLI must not refetch")
				}
				if result.Result.Receipt != nil && result.Result.Receipt.Complete {
					require.Equal(t, "page_delivered", result.Result.State)
					require.Equal(t, 2, result.Fetches)
					completed = true
					break
				}
			}
			require.True(t, completed)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				pages, err := f.repo.DiscoveryJob.Pages(ctx, f.listing.UUID, 0, 10)
				require.NoError(t, err)
				require.Len(t, pages, 2)
				require.False(t, pages[0].Complete)
				require.True(t, pages[1].Complete)
				for _, page := range pages {
					attempts, err := f.repo.ArchiveJob.Attempts(ctx, page.JobUUID, 0, 10)
					require.NoError(t, err)
					require.Len(t, attempts, 1, "restart and lost responses cannot duplicate a fetched page")
				}
				return nil
			}))
		})
	}
}
