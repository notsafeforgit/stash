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
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestEnrichmentDispatchHTTPValidatesPaginationAndScope(t *testing.T) {
	f := newEnrichmentHTTPFixture(t)
	path := "/enrichment/collections/" + f.collection.UUID
	request := map[string]any{"policy_sha256": strings.Repeat("a", 64), "extractor_version": "1.32.15-dev", "after": 0, "limit": 20}
	job, err := f.worker.Admit(t.Context(), f.token, f.target.UUID, 1, request["policy_sha256"].(string), "1.32.15-dev")
	require.NoError(t, err)
	page := enrichmentHTTPValue[[]models.EnrichmentJobCandidate](t, f.request(t, "POST", path+"/jobs/ready", request, 200))
	require.Equal(t, []models.EnrichmentJobCandidate{{Sequence: job.Sequence, UUID: job.UUID}}, page)
	request["after"] = job.Sequence
	require.JSONEq(t, "[]", string(f.request(t, "POST", path+"/jobs/ready", request, 200)))
	for key, value := range map[string]any{"after": -1, "limit": 101, "policy_sha256": "invalid", "extractor_version": ""} {
		good := request[key]
		request[key] = value
		f.request(t, "POST", path+"/jobs/ready", request, 400)
		request[key] = good
	}
	f.request(t, "POST", path+"/ready", map[string]any{"after": models.EnrichmentTargetCursor{Priority: 20, NotBefore: f.now, UUID: uuid.NewString()}, "limit": 20}, 200)
	f.request(t, "POST", path+"/ready", map[string]any{"after": map[string]any{"priority": 101}, "limit": 20}, 400)
	var other *models.SourceCollection
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		other, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Other feed", Kind: "feed", Namespace: "native:reddit", State: "active"}})
		return err
	}))
	_, unrelated, err := f.service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: other.UUID}}, nil)
	require.NoError(t, err)
	f.token = unrelated
	f.request(t, "POST", path+"/jobs/ready", request, 403)
	f.token = "missing"
	f.request(t, "POST", path+"/jobs/ready", request, 401)
}

func TestEnrichmentMaintenanceRuntimeRecoversWithoutOtherWorkers(t *testing.T) {
	f := newEnrichmentHTTPFixture(t)
	job, err := f.worker.Admit(t.Context(), f.token, f.target.UUID, 1, strings.Repeat("a", 64), "1.32.15-dev")
	require.NoError(t, err)
	running, err := f.worker.Claim(t.Context(), f.token, job.UUID, job.Revision, uuid.NewString(), strings.Repeat("a", 64), "1.32.15-dev", time.Minute)
	require.NoError(t, err)
	worker := ingest.NewEnrichmentMaintenance(f.service)
	worker.Now = func() time.Time { return running.LeaseUntil.Add(time.Second) }
	worker.PollInterval = 10 * time.Millisecond
	runtime := &archiveWorkerRuntime{worker: worker}
	runtime.start()
	t.Cleanup(runtime.stop)
	require.Eventually(t, func() bool {
		value, err := f.worker.Find(t.Context(), f.token, job.UUID)
		return err == nil && value.State == "queued" && value.ErrorCode == "lease_expired"
	}, 10*time.Second, 10*time.Millisecond)
	runtime.stop()
	select {
	case <-runtime.done:
	default:
		t.Fatal("maintenance did not stop with the server runtime")
	}
	runtime.start()
	require.True(t, runtime.closed)
}

func TestPythonEnrichmentDispatchRecoversLostAdmissionsAndExpiredDelivery(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	for _, scenario := range []string{"lost_responses", "expired_delivery"} {
		t.Run(scenario, func(t *testing.T) {
			f := newEnrichmentHTTPFixture(t)
			var now atomic.Int64
			now.Store(f.now.UnixMilli())
			f.worker.Now = func() time.Time { return time.UnixMilli(now.Load()).UTC() }
			var mu sync.Mutex
			lost := map[string]bool{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Date", f.worker.Now().Format(http.TimeFormat))
				for _, suffix := range []string{"/jobs", "/checkpoint", "/publish"} {
					if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, suffix) {
						continue
					}
					mu.Lock()
					drop := !lost[suffix] && (scenario == "lost_responses" || suffix == "/checkpoint")
					if drop {
						lost[suffix] = true
					}
					mu.Unlock()
					if drop {
						if scenario == "lost_responses" {
							committed := httptest.NewRecorder()
							f.handler.ServeHTTP(committed, r)
							if committed.Code != 200 {
								w.WriteHeader(committed.Code)
								_, _ = w.Write(committed.Body.Bytes())
								return
							}
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(503)
						_, _ = io.WriteString(w, `{"error":"temporarily_unavailable"}`)
						return
					}
				}
				f.handler.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			directory := t.TempDir()
			fixture, err := filepath.Abs("../../pkg/archive/testdata/enrichment-transcript-v1.json")
			require.NoError(t, err)
			clock := 1000
			run := func(delivery, forbid bool, expected string) int {
				t.Helper()
				clock += 1000
				setup, err := json.Marshal(map[string]any{"endpoint": server.URL, "producer": f.producer.UUID, "collection": f.collection.UUID,
					"directory": directory, "fixture": fixture, "clock": clock, "delivery_only": delivery, "forbid_fetch": forbid, "expected": expected})
				require.NoError(t, err)
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests/http_enrichment_dispatch.py"))
				command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_INGEST_TOKEN="+f.token)
				if !delivery {
					command.Env = append(command.Env, "ENRICHMENT_DISPATCH_LOGIN=fixture-private-login")
				}
				command.Stdin = bytes.NewReader(setup)
				var stdout, stderr bytes.Buffer
				command.Stdout, command.Stderr = &stdout, &stderr
				require.NoError(t, command.Run(), stderr.String())
				require.NotContains(t, stdout.String(), "fixture-private-login")
				require.NotContains(t, stderr.String(), "fixture-private-login")
				var result struct {
					Fetches int `json:"fetches"`
				}
				require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
				return result.Fetches
			}
			if scenario == "lost_responses" {
				require.Zero(t, run(false, true, "unavailable"))
				require.Equal(t, 1, run(false, false, "delivery_pending"))
				require.Equal(t, 1, run(true, true, "delivery_pending"))
				require.Equal(t, 1, run(true, true, "completed"))
			} else {
				require.Equal(t, 1, run(false, false, "delivery_pending"))
				var job *models.ArchiveJob
				require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
					binding, err := f.repo.EnrichmentJob.TargetBinding(ctx, f.target.UUID, 1)
					if err != nil {
						return err
					}
					job, err = f.repo.ArchiveJob.Find(ctx, binding.JobUUID)
					return err
				}))
				now.Store(job.LeaseUntil.Add(time.Second).UnixMilli())
				maintenance := ingest.NewEnrichmentMaintenance(f.service)
				maintenance.Now = f.worker.Now
				result, err := maintenance.Process(t.Context())
				require.NoError(t, err)
				require.Equal(t, 1, result.Recovered)
				current, err := f.worker.Find(t.Context(), f.token, job.UUID)
				require.NoError(t, err)
				now.Store(current.AvailableAt.UnixMilli())
				require.Equal(t, 1, run(false, true, "completed"))
			}
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				binding, err := f.repo.EnrichmentJob.TargetBinding(ctx, f.target.UUID, 1)
				require.NoError(t, err)
				publication, err := f.repo.EnrichmentJob.Publication(ctx, binding.JobUUID)
				require.NoError(t, err)
				require.NotNil(t, publication)
				job, err := f.repo.ArchiveJob.Find(ctx, binding.JobUUID)
				require.NoError(t, err)
				require.Equal(t, "succeeded", job.State)
				return nil
			}))
		})
	}
}
