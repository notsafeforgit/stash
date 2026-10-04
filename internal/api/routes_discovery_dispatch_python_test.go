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

	"github.com/google/uuid"
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
	for _, scenario := range []string{"lost_admission", "lost_page", "filtered_page", "global_profile"} {
		t.Run(scenario, func(t *testing.T) {
			f := newDiscoveryHTTPFixtureForPolicy(t, time.Date(2026, 10, 4, 14, 0, 0, 0, time.UTC), policy.Digest)
			worker := ingest.NewDiscoveryCoordinator(f.service)
			var now atomic.Int64
			now.Store(f.now.UnixMilli())
			worker.Now = func() time.Time { return time.UnixMilli(now.Load()).UTC() }
			f.handler = (&ingestRoutes{service: f.service, discovery: worker}).router()
			var root *models.MediaRoot
			if scenario == "global_profile" {
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					var err error
					root, err = f.repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Discovery root", State: "active"}})
					if err != nil {
						return err
					}
					definition := f.collection.SourceCollectionDefinition
					definition.RootUUID, definition.PathPrefix = &root.UUID, "."
					f.collection, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
					if err != nil {
						return err
					}
					input := f.listing.DiscoveryListingInput
					input.UUID, input.RootUUID, input.CollectionRevision = uuid.NewString(), &root.UUID, f.collection.Revision
					f.listing, err = f.repo.DiscoveryJob.CreateListing(ctx, input, worker.Now())
					return err
				}))
				_, f.token, err = f.service.IssueCredential(t.Context(), f.producer.UUID, nil, nil, root.UUID)
				require.NoError(t, err)
			}
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
				if (scenario == "lost_admission" || scenario == "lost_page") && r.Method == "POST" && strings.HasSuffix(r.URL.Path, suffix) && lost.CompareAndSwap(false, true) {
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
			workerID := uuid.NewString()
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
					"directory": directory, "fixture": fixture, "clock": 1000 + cycle*60, "delivery_only": delivery,
					"global": scenario == "global_profile", "worker_uuid": workerID}, f.token, !delivery)
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
			case "global_profile":
				require.Equal(t, "page_delivered", first.Result.State)
				require.Equal(t, 1, first.Fetches)
			}
			listings := []string{f.listing.UUID}
			if scenario == "global_profile" {
				// The original root grant must discover a later registration without
				// replacing the local profile list or restarting native scheduling.
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					collection, err := f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
						Label: "Later collection", Kind: "feed", Namespace: "native:reddit", State: "active", RootUUID: &root.UUID, PathPrefix: "."}})
					if err != nil {
						return err
					}
					input := f.listing.DiscoveryListingInput
					input.UUID, input.CollectionUUID, input.CollectionRevision = uuid.NewString(), collection.UUID, collection.Revision
					listing, err := f.repo.DiscoveryJob.CreateListing(ctx, input, worker.Now())
					if err == nil {
						listings = append(listings, listing.UUID)
					}
					return err
				}))
			}
			completed := false
			for cycle := 1; cycle <= 16; cycle++ {
				now.Add(int64(2 * time.Minute / time.Millisecond))
				result := run(cycle, scenario == "lost_page" && cycle == 1)
				require.Contains(t, []string{"waiting", "idle", "page_delivered"}, result.Result.State)
				if scenario == "lost_page" && cycle == 1 {
					require.Equal(t, "page_delivered", result.Result.State)
					require.Equal(t, 1, result.Fetches, "delivery-only CLI must not refetch")
				}
				if result.Result.Receipt != nil && result.Result.Receipt.Complete {
					require.Equal(t, "page_delivered", result.Result.State)
					completed = true
					require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
						for _, id := range listings {
							head, err := f.repo.DiscoveryJob.PageHead(ctx, id)
							if err != nil {
								return err
							}
							completed = completed && head != nil && head.Complete
						}
						return nil
					}))
					if completed {
						require.Equal(t, 2*len(listings), result.Fetches)
						break
					}
				}
			}
			require.True(t, completed)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				for _, id := range listings {
					pages, err := f.repo.DiscoveryJob.Pages(ctx, id, 0, 10)
					require.NoError(t, err)
					require.Len(t, pages, 2)
					require.False(t, pages[0].Complete)
					require.True(t, pages[1].Complete)
					for _, page := range pages {
						attempts, err := f.repo.ArchiveJob.Attempts(ctx, page.JobUUID, 0, 10)
						require.NoError(t, err)
						require.Len(t, attempts, 1, "restart and lost responses cannot duplicate a fetched page")
					}
				}
				return nil
			}))
		})
	}
}
