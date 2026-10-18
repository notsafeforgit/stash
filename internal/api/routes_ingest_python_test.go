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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/internal/testclients"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func nativeProducerRuntime(t *testing.T) (string, string) {
	t.Helper()
	return testclients.Runtime(t, "gallery-dl")
}

func TestPythonProducerDurableDeliveryAgainstNativeHTTP(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "library.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	service := ingest.New(db.Repository())
	var err error
	var producer *models.IngestProducer
	var collection *models.SourceCollection
	require.NoError(t, service.Repo.WithTxn(context.Background(), func(ctx context.Context) error {
		producer, err = service.Repo.Ingest.CreateProducer(ctx, "Python fixture")
		if err != nil {
			return err
		}
		collection, err = service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Fixture feed", Kind: "feed", Namespace: "native:twitter", State: "active", TargetURL: "https://x.com/fixture/media",
		}, Origin: "review"})
		return err
	}))
	_, token, err := service.IssueCredential(context.Background(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID}}, nil)
	require.NoError(t, err)
	router := (&ingestRoutes{service: service}).router()
	var batches, submissions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lose := (r.URL.Path == ingestPath+"/batches" && batches.Add(1) == 1) ||
			(r.URL.Path == ingestPath+"/runs" && r.Method == http.MethodPost && submissions.Add(1) == 1)
		if lose {
			// Commit the real request, then replace the response as a failed proxy
			// could. The next delivery must recover the immutable receipt.
			committed := httptest.NewRecorder()
			router.ServeHTTP(committed, r)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":"temporarily_unavailable"}`)
			return
		}
		router.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	setup, err := json.Marshal(map[string]interface{}{
		"directory": t.TempDir(), "endpoint": server.URL, "producer": producer.UUID,
		"collection": collection.UUID, "revision": collection.Revision,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests/http_interop.py"))
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_INGEST_TOKEN="+token)
	command.Stdin = bytes.NewReader(setup)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	require.NoError(t, command.Run(), stderr.String())
	var result struct {
		Accepted string `json:"accepted"`
		Rejected string `json:"rejected"`
		Digest   string `json:"sha256"`
		RunUUID  string `json:"run_uuid"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.NoError(t, service.Repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		accepted, err := service.Repo.Ingest.FindReceipt(ctx, producer.UUID, result.Accepted)
		require.NoError(t, err)
		require.NotNil(t, accepted)
		require.Equal(t, result.Digest, accepted.Digest)
		rejected, err := service.Repo.Ingest.FindReceipt(ctx, producer.UUID, result.Rejected)
		require.NoError(t, err)
		require.Nil(t, rejected)
		runs, err := service.Repo.SourceRun.List(ctx, collection.UUID, []*string{nil}, 0, 50)
		require.NoError(t, err)
		require.Len(t, runs, 1, "lost submission acknowledgement must not create another run")
		require.Equal(t, result.RunUUID, runs[0].UUID)
		require.Equal(t, "succeeded", runs[0].State)
		return nil
	}))
	require.EqualValues(t, 2, batches.Load())
	require.EqualValues(t, 2, submissions.Load())
}
