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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stretchr/testify/require"
)

func TestPythonDiscoveryExecutionRecoversOwnedPagesWithoutRefetch(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	script := filepath.Join(packagePath, "tests/http_discovery_execution.py")
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
	require.True(t, archive.ValidSHA256(policy.Digest))
	fixture, err := filepath.Abs("../../pkg/archive/testdata/discovery-pages-v1.json")
	require.NoError(t, err)
	for _, scenario := range []string{"lost_page", "lost_claim", "source_failure", "expired_page", "paused_after_fetch", "capacity", "rejected_page", "other_completion", "empty_final", "profile_mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			f := newDiscoveryHTTPFixtureForPolicy(t, time.Date(2026, 10, 4, 14, 0, 0, 0, time.UTC), policy.Digest)
			worker := ingest.NewDiscoveryCoordinator(f.service)
			var now atomic.Int64
			now.Store(f.now.UnixMilli())
			worker.Now = func() time.Time { return time.UnixMilli(now.Load()).UTC() }
			f.handler = (&ingestRoutes{service: f.service, discovery: worker}).router()
			job := f.admit(t)
			var mu sync.Mutex
			lost := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Date", worker.Now().Format(http.TimeFormat))
				page := r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/page")
				if scenario == "rejected_page" && page {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(422)
					_, _ = io.WriteString(w, `{"error":"invalid_discovery_work"}`)
					return
				}
				loseBefore := page && (scenario == "expired_page" || scenario == "other_completion")
				loseAfter := (scenario == "lost_page" && page) || (scenario == "lost_claim" && strings.HasSuffix(r.URL.Path, "/claim")) ||
					(scenario == "source_failure" && strings.HasSuffix(r.URL.Path, "/failure"))
				mu.Lock()
				lose := r.Method == "POST" && !lost && (loseBefore || loseAfter)
				if lose {
					lost = true
				}
				mu.Unlock()
				if lose {
					if !loseBefore {
						committed := httptest.NewRecorder()
						f.handler.ServeHTTP(committed, r)
						require.Equal(t, 200, committed.Code, committed.Body.String())
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(503)
					_, _ = io.WriteString(w, `{"error":"temporarily_unavailable"}`)
					return
				}
				f.handler.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			directory := t.TempDir()
			type executionResult struct {
				State   string `json:"state"`
				Fetches int    `json:"fetches"`
				Journal *struct {
					Phase       string `json:"phase"`
					StagedBytes int    `json:"staged_bytes"`
					Pending     string `json:"pending"`
				} `json:"journal"`
			}
			run := func(fetch string, deliver bool, expected string, maxBytes int) executionResult {
				t.Helper()
				body := invoke(t, map[string]any{"endpoint": server.URL, "producer": f.producer.UUID, "directory": directory,
					"fixture": fixture, "job_uuid": job.UUID, "fetch": fetch, "deliver_only": deliver, "expected": expected,
					"max_bytes": maxBytes, "pause_after_fetch": scenario == "paused_after_fetch", "mismatch": scenario == "profile_mismatch"}, f.token, !deliver)
				var result executionResult
				require.NoError(t, json.Unmarshal(body, &result))
				verifyNativeArchiveJournals(t, f.database, directory, server.URL)
				return result
			}
			fetch, expected, quota := "page", "delivery_pending", 512<<20
			switch scenario {
			case "source_failure":
				fetch = "source_failure"
			case "capacity":
				expected, quota = "capacity", archive.MaxEnrichmentTranscriptBytes-1
			case "paused_after_fetch":
				expected = "waiting"
			case "rejected_page":
				expected = "review"
			case "empty_final":
				fetch, expected = "empty_final", "page_delivered"
			case "profile_mismatch":
				expected = "profile_mismatch"
			}
			first := run(fetch, false, expected, quota)
			current, err := worker.Describe(t.Context(), f.token, job.UUID)
			require.NoError(t, err)
			expectedAttempts, expectedFetches := 1, 1
			switch scenario {
			case "profile_mismatch":
				require.Nil(t, first.Journal)
				require.Zero(t, first.Fetches)
				require.Equal(t, "queued", current.Job.State)
				require.Zero(t, current.Job.Fence)
				return
			case "rejected_page":
				require.Greater(t, first.Journal.StagedBytes, 0)
				require.Equal(t, "review", first.Journal.Phase)
				again := run("forbidden", true, "review", 512<<20)
				require.Equal(t, first.Journal.StagedBytes, again.Journal.StagedBytes)
				require.Equal(t, 1, again.Fetches)
				require.Nil(t, current.Receipt)
				return
			case "lost_claim":
				require.Zero(t, first.Fetches)
				run("page", false, "page_delivered", 512<<20)
			case "lost_page", "paused_after_fetch":
				require.Greater(t, first.Journal.StagedBytes, 0)
				run("forbidden", true, "page_delivered", 512<<20)
			case "source_failure", "capacity":
				require.Zero(t, first.Journal.StagedBytes)
				if scenario == "source_failure" {
					require.Equal(t, "failure", first.Journal.Pending)
					run("forbidden", true, "retry", 512<<20)
					expectedFetches = 2
				} else {
					require.Zero(t, first.Fetches)
				}
				now.Store(current.Job.AvailableAt.UnixMilli())
				run("page", false, "page_delivered", 512<<20)
				expectedAttempts = 2
			case "expired_page", "other_completion":
				require.Greater(t, first.Journal.StagedBytes, 0)
				now.Store(current.Job.LeaseUntil.Add(time.Second).UnixMilli())
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					count, err := f.repo.ArchiveJob.Recover(ctx, worker.Now(), 10)
					require.Equal(t, 1, count)
					return err
				}))
				current, err = worker.Describe(t.Context(), f.token, job.UUID)
				require.NoError(t, err)
				now.Store(current.Job.AvailableAt.UnixMilli())
				if scenario == "other_completion" {
					peer, err := worker.Claim(t.Context(), f.token, job.UUID, current.Job.Revision, uuid.NewString(), policy.Digest, f.listing.ExtractorVersion, time.Minute)
					require.NoError(t, err)
					_, err = worker.AppendPage(t.Context(), f.token, peer.Lease(), 1, f.page)
					require.NoError(t, err)
					again := run("forbidden", true, "review", 512<<20)
					require.Equal(t, first.Journal.StagedBytes, again.Journal.StagedBytes)
					require.Equal(t, 1, again.Fetches)
					return
				}
				run("forbidden", true, "ownership_required", 512<<20)
				run("forbidden", false, "page_delivered", 512<<20)
				expectedAttempts = 2
			}
			done := run("forbidden", true, "page_delivered", 512<<20)
			require.Equal(t, expectedFetches, done.Fetches)
			require.Equal(t, "completed", done.Journal.Phase)
			require.Zero(t, done.Journal.StagedBytes)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				attempts, err := f.repo.ArchiveJob.Attempts(ctx, job.UUID, 0, 10)
				require.NoError(t, err)
				require.Len(t, attempts, expectedAttempts)
				page, err := f.repo.DiscoveryJob.Page(ctx, f.listing.UUID, 1)
				require.NoError(t, err)
				require.NotNil(t, page)
				require.Equal(t, scenario == "empty_final", page.Complete)
				if scenario != "empty_final" {
					require.Contains(t, string(page.Body), "0.123456789012345678901234567890")
				}
				return nil
			}))
		})
	}
}
