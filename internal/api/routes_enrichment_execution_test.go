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
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestPythonEnrichmentExecutionRestartsAndPreservesSourceEvidence(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	for _, scenario := range []string{"lost_deliveries", "pending_child", "expired_pending", "lost_claim", "divergent_head", "capacity", "paused_after_fetch", "rejected_checkpoint", "identity_review"} {
		t.Run(scenario, func(t *testing.T) {
			reference := models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}
			if scenario == "identity_review" {
				reference = models.SourcePostIdentifier{Namespace: "legacy:catalog:" + uuid.NewString(), Value: "filename-only"}
			}
			f := newEnrichmentHTTPFixtureForPost(t, reference)
			var now atomic.Int64
			now.Store(f.now.UnixMilli())
			f.worker.Now = func() time.Time { return time.UnixMilli(now.Load()).UTC() }
			var mu sync.Mutex
			lost := map[string]bool{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Date", f.worker.Now().Format(http.TimeFormat))
				if scenario == "rejected_checkpoint" && r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/checkpoint") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnprocessableEntity)
					_, _ = io.WriteString(w, `{"error":"invalid_checkpoint"}`)
					return
				}
				for _, suffix := range []string{"/checkpoint", "/publish", "/failure", "/claim"} {
					if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, suffix) {
						continue
					}
					before := (scenario == "expired_pending" || scenario == "divergent_head") && suffix == "/checkpoint"
					mu.Lock()
					drop := before && !lost[suffix]
					if drop {
						lost[suffix] = true
					}
					mu.Unlock()
					if drop {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(503)
						_, _ = io.WriteString(w, `{"error":"temporarily_unavailable"}`)
						return
					}
					committed := httptest.NewRecorder()
					f.handler.ServeHTTP(committed, r)
					mu.Lock()
					lose := committed.Code == 200 && !lost[suffix] &&
						((scenario == "lost_deliveries" && (suffix == "/checkpoint" || suffix == "/publish")) ||
							(scenario == "pending_child" && suffix == "/failure") || (scenario == "lost_claim" && suffix == "/claim"))
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
			directory := t.TempDir()
			fixture, err := filepath.Abs("../../pkg/archive/testdata/enrichment-transcript-v1.json")
			require.NoError(t, err)
			type executionResult struct {
				JobUUID string `json:"job_uuid"`
				State   string `json:"state"`
				Fetches int    `json:"fetches"`
				Journal struct {
					Phase       string `json:"phase"`
					StagedBytes int    `json:"staged_bytes"`
					Pending     string `json:"pending"`
				} `json:"journal"`
			}
			run := func(fetch string, deliver, resumed bool, expected string, maxBytes int) executionResult {
				t.Helper()
				setup, err := json.Marshal(map[string]any{"endpoint": server.URL, "producer": f.producer.UUID, "collection": f.collection.UUID,
					"directory": directory, "fixture": fixture, "fetch": fetch, "deliver_only": deliver, "resumed": resumed,
					"expected": expected, "max_bytes": maxBytes, "pause_after_fetch": scenario == "paused_after_fetch"})
				require.NoError(t, err)
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests/http_enrichment_execution.py"))
				command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_INGEST_TOKEN="+f.token)
				if !deliver {
					command.Env = append(command.Env, "ENRICHMENT_FIXTURE_LOGIN=fixture-private-site-token")
				}
				command.Stdin = bytes.NewReader(setup)
				var stdout, stderr bytes.Buffer
				command.Stdout, command.Stderr = &stdout, &stderr
				require.NoError(t, command.Run(), stderr.String())
				require.NotContains(t, stdout.String(), "fixture-private-site-token")
				require.NotContains(t, stderr.String(), "fixture-private-site-token")
				var result executionResult
				require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
				return result
			}
			fetch, expected, quota := "complete", "delivery_pending", 512<<20
			if scenario == "pending_child" {
				fetch = "initial"
			}
			if scenario == "capacity" {
				expected, quota = "capacity", archive.MaxEnrichmentTranscriptBytes-1
			}
			if scenario == "paused_after_fetch" {
				expected = "waiting"
			}
			if scenario == "rejected_checkpoint" || scenario == "identity_review" {
				expected = "review"
			}
			first := run(fetch, false, false, expected, quota)
			job, err := f.worker.Find(t.Context(), f.token, first.JobUUID)
			require.NoError(t, err)
			expectedAttempts, expectedFetches := 1, 1
			switch scenario {
			case "identity_review":
				require.Equal(t, "failed", job.State)
				require.Equal(t, "review", first.Journal.Phase)
				head, err := f.worker.CheckpointHead(t.Context(), f.token, job.UUID)
				require.NoError(t, err)
				require.NotNil(t, head)
				publication, err := f.worker.Publication(t.Context(), f.token, job.UUID)
				require.NoError(t, err)
				require.Nil(t, publication)
				retained := run("forbidden", true, false, "review", 512<<20)
				require.Equal(t, 1, retained.Fetches)
				return
			case "paused_after_fetch":
				require.Greater(t, first.Journal.StagedBytes, 0)
				head, err := f.worker.CheckpointHead(t.Context(), f.token, job.UUID)
				require.NoError(t, err)
				require.Nil(t, head)
				run("forbidden", true, false, "completed", 512<<20)
			case "rejected_checkpoint":
				require.Greater(t, first.Journal.StagedBytes, 0)
				require.Equal(t, "review", first.Journal.Phase)
				retained := run("forbidden", true, false, "review", 512<<20)
				require.Equal(t, first.Journal.StagedBytes, retained.Journal.StagedBytes)
				require.Equal(t, 1, retained.Fetches)
				return
			case "lost_deliveries":
				require.Greater(t, first.Journal.StagedBytes, 0)
				second := run("forbidden", true, false, "delivery_pending", 512<<20)
				require.Zero(t, second.Journal.StagedBytes)
				require.Equal(t, "publish", second.Journal.Pending)
				run("forbidden", true, false, "completed", 512<<20)
			case "pending_child", "capacity":
				require.Zero(t, first.Journal.StagedBytes)
				if scenario == "pending_child" {
					require.Equal(t, "failure", first.Journal.Pending)
					run("forbidden", true, false, "retry", 512<<20)
					expectedFetches = 2
				} else {
					require.Zero(t, first.Fetches)
				}
				now.Store(job.AvailableAt.UnixMilli())
				run("complete", false, scenario == "pending_child", "completed", 512<<20)
				expectedAttempts = 2
			case "lost_claim":
				require.Zero(t, first.Fetches)
				run("complete", false, false, "completed", 512<<20)
			case "expired_pending", "divergent_head":
				require.Greater(t, first.Journal.StagedBytes, 0)
				now.Store(job.LeaseUntil.Add(time.Second).UnixMilli())
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					count, err := f.repo.ArchiveJob.Recover(ctx, f.worker.Now(), 10)
					require.Equal(t, 1, count)
					return err
				}))
				job, err = f.worker.Find(t.Context(), f.token, job.UUID)
				require.NoError(t, err)
				now.Store(job.AvailableAt.UnixMilli())
				if scenario == "divergent_head" {
					work, err := archive.DecodeEnrichmentJob(job)
					require.NoError(t, err)
					peer, err := f.worker.Claim(t.Context(), f.token, job.UUID, job.Revision, uuid.NewString(), work.PolicySHA256, work.ExtractorVersion, time.Minute)
					require.NoError(t, err)
					_, err = f.worker.Checkpoint(t.Context(), f.token, peer.Lease(), 0, bytes.ReplaceAll(f.complete, []byte("Shared caption"), []byte("A different observation")))
					require.NoError(t, err)
					_, err = f.worker.Fail(t.Context(), f.token, peer.Lease(), "worker_failed")
					require.NoError(t, err)
					job, err = f.worker.Find(t.Context(), f.token, job.UUID)
					require.NoError(t, err)
					now.Store(job.AvailableAt.UnixMilli())
					review := run("forbidden", false, false, "review", 512<<20)
					require.Greater(t, review.Journal.StagedBytes, 0)
					publication, err := f.worker.Publication(t.Context(), f.token, job.UUID)
					require.NoError(t, err)
					require.Nil(t, publication)
					return
				}
				run("forbidden", false, false, "completed", 512<<20)
				expectedAttempts = 2
			}
			final := run("forbidden", true, false, "completed", 512<<20)
			require.Equal(t, expectedFetches, final.Fetches)
			require.Equal(t, "completed", final.Journal.Phase)
			require.Zero(t, final.Journal.StagedBytes)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				attempts, err := f.repo.ArchiveJob.Attempts(ctx, first.JobUUID, 0, 10)
				require.NoError(t, err)
				require.Len(t, attempts, expectedAttempts)
				publication, err := f.repo.EnrichmentJob.Publication(ctx, first.JobUUID)
				require.NoError(t, err)
				require.NotNil(t, publication)
				records, err := f.repo.EnrichmentJob.PublishedRecords(ctx, first.JobUUID, -1, 100)
				require.NoError(t, err)
				require.Len(t, records, publication.RecordCount)
				for _, record := range records {
					require.Equal(t, f.producer.UUID, record.ProducerUUID)
				}
				if scenario == "pending_child" {
					require.EqualValues(t, 1, records[0].Fence)
					require.EqualValues(t, 2, records[len(records)-1].Fence)
				}
				return nil
			}))
		})
	}
}
