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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonProducerDurableDeliveryAgainstNativeHTTP(t *testing.T) {
	python, err := exec.LookPath("python3")
	require.NoError(t, err, "native producer integration tests require Python 3.12 or newer")
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "library.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	service := ingest.New(db.Repository())
	var producer *models.IngestProducer
	var collection *models.SourceCollection
	require.NoError(t, service.Repo.WithTxn(context.Background(), func(ctx context.Context) error {
		producer, err = service.Repo.Ingest.CreateProducer(ctx, "Python fixture")
		if err != nil {
			return err
		}
		collection, err = service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Fixture feed", Kind: "feed", Namespace: "native:twitter", State: "active",
		}, Origin: "review"})
		return err
	}))
	_, token, err := service.IssueCredential(context.Background(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID}}, nil)
	require.NoError(t, err)
	router := (&ingestRoutes{service: service}).router()
	var batches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ingestPath+"/batches" && batches.Add(1) == 1 {
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
	packagePath, err := filepath.Abs("../../integrations/gallery-dl")
	require.NoError(t, err)
	// Register Python inputs with Go's test cache. Files read only by the child
	// interpreter would otherwise let a changed producer reuse an old result.
	for _, directory := range []string{"src/stash_ingest", "tests"} {
		entries, err := os.ReadDir(filepath.Join(packagePath, directory))
		require.NoError(t, err)
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".py") {
				_, err := os.ReadFile(filepath.Join(packagePath, directory, entry.Name()))
				require.NoError(t, err)
			}
		}
	}
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
		return nil
	}))
	require.EqualValues(t, 2, batches.Load())
}
