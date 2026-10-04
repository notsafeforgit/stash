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
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func exercisePythonDiscoveryDetailHTTP(t *testing.T, service *ingest.Service, token string, review models.DiscoveryMatchReview, body json.RawMessage) {
	t.Helper()
	python, packagePath := nativeProducerRuntime(t)
	credential, err := service.Authenticate(t.Context(), token)
	require.NoError(t, err)
	var collection string
	require.NoError(t, service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		listing, err := service.Repo.DiscoveryJob.Listing(ctx, review.Target.ListingUUID)
		if err == nil {
			collection = listing.CollectionUUID
		}
		return err
	}))
	worker := ingest.NewDiscoveryDetailCoordinator(service)
	now := time.Now().UTC().Truncate(time.Millisecond)
	worker.Now = func() time.Time { return now }
	handler := (&ingestRoutes{service: service, detail: worker}).router()
	var mu sync.Mutex
	lost := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", now.Format(http.TimeFormat))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		suffix := ""
		for _, candidate := range []string{"/checkpoint", "/complete"} {
			if strings.HasSuffix(r.URL.Path, candidate) {
				suffix = candidate
			}
		}
		if review.Coverage.Complete && strings.HasSuffix(r.URL.Path, "/jobs") {
			suffix = "/jobs"
		}
		mu.Lock()
		drop := suffix != "" && r.Method == "POST" && recorder.Code == http.StatusOK && !lost[suffix]
		if drop {
			lost[suffix] = true
		}
		mu.Unlock()
		if drop {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":"temporarily_unavailable"}`)
			return
		}
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	defer server.Close()
	directory := t.TempDir()
	type result struct {
		JobUUID string `json:"job_uuid"`
		State   string `json:"state"`
		Fetches int    `json:"fetches"`
		Journal struct {
			Phase       string                        `json:"phase"`
			Pending     string                        `json:"pending"`
			StagedBytes int                           `json:"staged_bytes"`
			Comparison  *models.DiscoveryDetailResult `json:"comparison"`
		} `json:"journal"`
	}
	run := func(deliver bool, expected string) result {
		t.Helper()
		input, err := json.Marshal(map[string]any{"directory": directory, "endpoint": server.URL, "producer": credential.ProducerUUID,
			"target": review.Target.UUID, "revision": review.Target.Revision, "candidate": review.Candidate.Sequence,
			"collection": collection, "automatic": review.Coverage.Complete,
			"body": body, "deliver_only": deliver, "expected": expected})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests/http_discovery_detail_execution.py"))
		command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_INGEST_TOKEN="+token)
		if !deliver {
			command.Env = append(command.Env, "DETAIL_FIXTURE_LOGIN=fixture-private-site-token")
		}
		command.Stdin = bytes.NewReader(input)
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		return enrichmentHTTPValue[result](t, output)
	}
	if review.Coverage.Complete {
		missed := run(false, "unavailable")
		require.Zero(t, missed.Fetches, "an uncertain admission cannot begin source access")
		require.Empty(t, missed.JobUUID, "the restarted dispatcher must discover the committed job")
	}
	first := run(false, "delivery_pending")
	require.Equal(t, "checkpoint", first.Journal.Pending)
	require.Positive(t, first.Journal.StagedBytes)
	second := run(true, "delivery_pending")
	require.Equal(t, first.JobUUID, second.JobUUID)
	require.Equal(t, "complete", second.Journal.Pending)
	require.Zero(t, second.Journal.StagedBytes)
	final := run(true, "completed")
	require.Equal(t, 1, final.Fetches)
	require.Equal(t, "completed", final.Journal.Phase)
	require.NotNil(t, final.Journal.Comparison)
	require.Equal(t, "corroborated", final.Journal.Comparison.Evidence.Status)
	require.Equal(t, final, run(true, "completed"))
	require.NoError(t, service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := service.Repo.DiscoveryMatch.Review(ctx, review.Target.UUID)
		require.NoError(t, err)
		review.Detail, review.Blockers = final.Journal.Comparison, []string{"history_not_retained"}
		if review.Coverage.Complete {
			review.Blockers = []string{}
		}
		require.Equal(t, review, *current, "comparison never accepts identity or clears coverage blockers")
		checkpoint, err := service.Repo.DiscoveryDetail.CheckpointHead(ctx, final.JobUUID)
		require.NoError(t, err)
		original, err := archive.ParseEnrichmentTranscript(body)
		require.NoError(t, err)
		require.Equal(t, original.Body(), checkpoint.Body, "a dropped reply never changes original observations")
		records, err := service.Repo.DiscoveryDetail.CheckpointRecords(ctx, final.JobUUID, -1, 100)
		require.NoError(t, err)
		require.Len(t, records, len(original.Records))
		for _, record := range records {
			require.Equal(t, credential.ProducerUUID, record.ProducerUUID)
		}
		return nil
	}))
}
