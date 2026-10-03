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

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestPythonEnrichmentWorkerHTTPReplaysLostResponses(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	for _, large := range []bool{false, true} {
		name := "ordinary"
		if large {
			name = "large_unicode_checkpoint"
		}
		t.Run(name, func(t *testing.T) {
			f := newEnrichmentHTTPFixture(t)
			// HTTP Date and coordinator deadlines must share the real clock;
			// large transcripts take longer under the race detector.
			f.worker.Now = time.Now
			var mu sync.Mutex
			lost := map[string]bool{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for _, suffix := range []string{"/jobs", "/claim", "/checkpoint", "/publish"} {
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
			fixture, err := filepath.Abs("../../pkg/archive/testdata/enrichment-transcript-v1.json")
			require.NoError(t, err)
			setup, err := json.Marshal(map[string]any{"endpoint": server.URL, "producer": f.producer.UUID, "collection": f.collection.UUID,
				"target": f.target.UUID, "fixture": fixture, "large": large})
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
			defer cancel()
			command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests/http_enrichment_interop.py"))
			command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_INGEST_TOKEN="+f.token)
			command.Stdin = bytes.NewReader(setup)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			require.NoError(t, command.Run(), stderr.String())
			var result struct {
				JobUUID         string                       `json:"job_uuid"`
				Publication     models.EnrichmentPublication `json:"publication"`
				CheckpointBytes int                          `json:"checkpoint_bytes"`
			}
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
			if large {
				require.Greater(t, result.CheckpointBytes, 4<<20)
			}
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				publication, err := f.repo.EnrichmentJob.Publication(ctx, result.JobUUID)
				require.NoError(t, err)
				require.Equal(t, result.Publication, *publication)
				attempts, err := f.repo.ArchiveJob.Attempts(ctx, result.JobUUID, 0, 10)
				require.NoError(t, err)
				require.Len(t, attempts, 1, "lost claim must not create another attempt")
				require.Equal(t, "succeeded", attempts[0].Outcome)
				head, err := f.repo.EnrichmentJob.CheckpointHead(ctx, result.JobUUID)
				require.NoError(t, err)
				require.Nil(t, head)
				release, err := f.repo.EnrichmentJob.CheckpointRelease(ctx, result.JobUUID)
				require.NoError(t, err)
				require.NotNil(t, release)
				return nil
			}))
			mu.Lock()
			require.Len(t, lost, 4)
			mu.Unlock()
		})
	}
}
