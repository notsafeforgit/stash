package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/file"
	imagefile "github.com/stashapp/stash/pkg/file/image"
	"github.com/stashapp/stash/pkg/hash/md5"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type ingestHTTPFingerprinter struct{}

func (ingestHTTPFingerprinter) CalculateFingerprints(_ *models.BaseFile, opener file.Opener, _ bool) ([]models.Fingerprint, error) {
	f, err := opener.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	digest, err := md5.FromReader(f)
	return []models.Fingerprint{{Type: models.FingerprintTypeMD5, Fingerprint: digest}}, err
}

func TestIngestHTTPFileReceiptWorkerAndShutdown(t *testing.T) {
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe required")
	}
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "ingest.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	service := ingest.New(repo)
	dir := t.TempDir()
	var media bytes.Buffer
	require.NoError(t, png.Encode(&media, image.NewRGBA(image.Rect(0, 0, 17, 23))))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "purchased.png"), media.Bytes(), 0600))
	binding, err := archive.ProbeMediaRoot(dir)
	require.NoError(t, err)
	var root *models.MediaRoot
	var collection *models.SourceCollection
	var producer *models.IngestProducer
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		root, err = repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Purchased media", State: "active", Binding: binding}})
		if err != nil {
			return err
		}
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Manual import", Kind: "manual_batch", State: "active", RootUUID: &root.UUID, PathPrefix: "."}})
		if err != nil {
			return err
		}
		producer, err = repo.Ingest.CreateProducer(ctx, "HTTP fixture")
		return err
	}))
	_, token, err := service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID, RootUUID: &root.UUID}}, nil)
	require.NoError(t, err)
	handler := withIngestRoutes(http.NotFoundHandler(), service, true)
	request := func(method, route string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, ingestPath+route, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	capabilities := request(http.MethodGet, "/capabilities", nil)
	require.Equal(t, http.StatusOK, capabilities.Code)
	require.Contains(t, capabilities.Body.String(), `"file_ingestion":true`)
	require.Contains(t, capabilities.Body.String(), `"file.completed"`)
	event := ingest.FileEvent{Protocol: 1, ProducerUUID: producer.UUID, EventUUID: uuid.NewString(), RunUUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, RootUUID: root.UUID,
		Kind: "file.completed", ObservedAt: time.Now().UTC(), RelativePath: "purchased.png", Size: int64(media.Len()), SHA256: ingest.Digest(media.Bytes()), MediaKind: models.ArchiveImage}
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	batch, err := json.Marshal(ingestBatch{Events: []ingestBatchEvent{{Digest: ingest.Digest(raw), Event: raw}, {Digest: "wrong", Event: raw}}})
	require.NoError(t, err)
	accepted := request(http.MethodPost, "/batches", batch)
	require.Equal(t, http.StatusOK, accepted.Code)
	var response struct {
		Results []ingestBatchResult `json:"results"`
	}
	require.NoError(t, json.Unmarshal(accepted.Body.Bytes(), &response))
	require.Len(t, response.Results, 2)
	require.Equal(t, http.StatusAccepted, response.Results[0].Status)
	require.Equal(t, http.StatusBadRequest, response.Results[1].Status)
	receipt := response.Results[0].Receipt
	require.NotEmpty(t, receipt.JobUUID)
	require.Empty(t, receipt.CaptureUUID)
	statusRoute := "/receipts/" + event.EventUUID + "/status"
	status := request(http.MethodGet, statusRoute, nil)
	require.Contains(t, status.Body.String(), `"state":"queued"`)
	require.Contains(t, status.Body.String(), `"registration_committed":false`)
	require.NotContains(t, status.Body.String(), dir)

	started, release := make(chan struct{}), make(chan struct{})
	scanner := &file.Scanner{FingerprintCalculator: ingestHTTPFingerprinter{}, FileDecorators: []file.Decorator{&imagefile.Decorator{FFProbe: ffmpeg.NewFFProbe(probe)}}}
	worker := ingest.NewFileWorker(service, func(models.ArchiveEntityKind) *file.Scanner { return scanner }, func(ctx context.Context, _ ingest.FileWork, _ ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
		close(started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
		}
		return guard(ctx)
	})
	runtime := &archiveWorkerRuntime{worker: worker}
	t.Cleanup(runtime.stop)
	runtime.start()
	runtime.start() // Starting twice must not create two processing loops.
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not reach postprocessing")
	}
	status = request(http.MethodGet, statusRoute, nil)
	require.Contains(t, status.Body.String(), `"state":"running"`)
	require.Contains(t, status.Body.String(), `"registration_committed":true`)
	require.Contains(t, status.Body.String(), `"media_ingested":false`)
	close(release)
	require.Eventually(t, func() bool {
		status = request(http.MethodGet, statusRoute, nil)
		var result ingest.ReceiptStatus
		return json.Unmarshal(status.Body.Bytes(), &result) == nil && result.State == "succeeded"
	}, 10*time.Second, 20*time.Millisecond)
	require.Contains(t, status.Body.String(), `"media_ingested":true`)
	immutable := request(http.MethodGet, "/receipts/"+event.EventUUID, nil)
	var original models.IngestReceipt
	require.NoError(t, json.Unmarshal(immutable.Body.Bytes(), &original))
	require.Equal(t, *receipt, original)
	replay := request(http.MethodPost, "/batches", batch)
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &response))
	require.Equal(t, receipt, response.Results[0].Receipt)
	runtime.stop()
	handler = withIngestRoutes(http.NotFoundHandler(), service, false)
	require.Contains(t, request(http.MethodGet, "/capabilities", nil).Body.String(), `"file_ingestion":false`)
	replay = request(http.MethodPost, "/batches", batch)
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &response))
	require.Equal(t, http.StatusAccepted, response.Results[0].Status)
	require.Equal(t, receipt, response.Results[0].Receipt, "a lost acknowledgement remains replayable without a configured processor")
	event.EventUUID = uuid.NewString()
	raw, err = json.Marshal(event)
	require.NoError(t, err)
	batch, err = json.Marshal(ingestBatch{Events: []ingestBatchEvent{{Digest: ingest.Digest(raw), Event: raw}}})
	require.NoError(t, err)
	rejected := request(http.MethodPost, "/batches", batch)
	response.Results = nil
	require.NoError(t, json.Unmarshal(rejected.Body.Bytes(), &response))
	require.Equal(t, http.StatusUnprocessableEntity, response.Results[0].Status)
	require.Nil(t, response.Results[0].Receipt, "an unavailable processor cannot accept new work")
	runtime.stop()
	runtime.start() // A stopped server cannot restart background writes.
	select {
	case <-runtime.done:
	default:
		t.Fatal("worker outlived shutdown")
	}
}

func TestIngestFileCapacityIsBackpressure(t *testing.T) {
	status, code := ingestErrorCode(models.ErrArchiveJobCapacity)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "queue_full", code)
}
