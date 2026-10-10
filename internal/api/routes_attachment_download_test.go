package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestAttachmentDownloadHTTPHistoryReplayAndPortableRestore(t *testing.T) {
	db, repo, get := sourcePostBrowserHTTPFixture(t)
	service := ingest.New(repo)
	binding, err := archive.ProbeMediaRoot(t.TempDir())
	require.NoError(t, err)
	var root *models.MediaRoot
	var collection *models.SourceCollection
	var producer *models.IngestProducer
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		root, err = repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Downloads", State: "active", Binding: binding}})
		if err != nil {
			return err
		}
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Source", Kind: "feed", State: "active", Namespace: "native:reddit", TargetURL: "https://www.reddit.com/user/example/submitted/", RootUUID: &root.UUID, PathPrefix: "."}})
		if err != nil {
			return err
		}
		producer, err = repo.Ingest.CreateProducer(ctx, "HTTP worker")
		return err
	}))
	_, token, err := service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID, RootUUID: &root.UUID}}, nil)
	require.NoError(t, err)
	coordinator := ingest.NewRunCoordinator(service)
	policy := strings.Repeat("a", 64)
	run, err := coordinator.Submit(t.Context(), token, models.SourceRunRequest{RequestUUID: uuid.NewString(), CollectionUUID: collection.UUID,
		CollectionRevision: collection.Revision, Operation: "download", PolicySHA256: policy, Window: models.SourceWindow{Until: time.Now().UTC().Truncate(time.Millisecond)}})
	require.NoError(t, err)
	run, err = coordinator.Claim(t.Context(), token, run.UUID, uuid.NewString(), policy, time.Minute)
	require.NoError(t, err)
	handler := withIngestRoutes(http.NotFoundHandler(), service, false)
	request := func(method, path, auth string, body any) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, bytes.NewReader(encoded))
		r.Header.Set("Content-Type", "application/json")
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	capabilities := request(http.MethodGet, ingestPath+"/capabilities", token, nil)
	require.Equal(t, http.StatusOK, capabilities.Code)
	require.Contains(t, capabilities.Body.String(), `"attachment_download_protocol":1`)
	require.Contains(t, capabilities.Body.String(), `"file_transformation_protocol":1`)
	require.Contains(t, capabilities.Body.String(), `"attachment.download"`)
	batch := func(event any, auth string) []ingestBatchResult {
		t.Helper()
		body, err := json.Marshal(event)
		require.NoError(t, err)
		item := ingestBatchEvent{Digest: ingest.Digest(body), Event: body}
		w := request(http.MethodPost, ingestPath+"/batches", auth, ingestBatch{Events: []ingestBatchEvent{item, item}})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var ret struct {
			Results []ingestBatchResult `json:"results"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ret))
		require.Len(t, ret.Results, 2)
		return ret.Results
	}
	capture := ingest.CaptureEvent{Protocol: 1, ProducerUUID: producer.UUID, EventUUID: uuid.NewString(), RunUUID: run.UUID,
		CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, RootUUID: &root.UUID, Kind: "source.capture", ObservedAt: time.Now().UTC(),
		ExtractorVersion: "fixture", RetentionPolicy: archive.SourceRetentionVersion, Post: ingest.PostReference{Namespace: "native:reddit", Value: "download-post"},
		Source: []byte(`{"category":"reddit","id":"download-post","url":"https://i.redd.it/media.jpg"}`)}
	ack := batch(capture, token)
	require.Equal(t, http.StatusOK, ack[0].Status)
	var attachment *models.SourceAttachment
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		attachment, err = repo.SourceAttachment.Lookup(ctx, ack[0].Receipt.PostUUID, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "media"})
		return err
	}))
	require.NotNil(t, attachment)
	event := ingest.AttachmentDownloadEvent{Protocol: 1, ProducerUUID: producer.UUID, EventUUID: uuid.NewString(), RunUUID: run.UUID,
		CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, RootUUID: root.UUID, Kind: "attachment.download", ObservedAt: time.Now().UTC(),
		OwnerUUID: run.OwnerUUID, Fence: run.Fence, TransferSequence: 3, CaptureEventUUID: capture.EventUUID,
		Attachment: ingest.PostReference{Namespace: attachment.Reference.Namespace, Value: attachment.Reference.Value}, State: "started"}
	require.Equal(t, http.StatusUnauthorized, request(http.MethodPost, ingestPath+"/batches", "", ingestBatch{}).Code)
	ack = batch(event, token)
	require.Equal(t, http.StatusOK, ack[0].Status)
	require.Equal(t, ack[0].Receipt, ack[1].Receipt)
	original := ack[0].Receipt
	foreign := event
	foreign.EventUUID, foreign.OwnerUUID = uuid.NewString(), uuid.NewString()
	require.Equal(t, http.StatusForbidden, batch(foreign, token)[0].Status)
	end := event
	end.EventUUID, end.State, end.ReasonCode = uuid.NewString(), "excluded", "unsupported_media"
	require.Equal(t, http.StatusOK, batch(end, token)[0].Status)
	path := "/attachments/" + attachment.UUID + "/download-history"
	var rows []models.AttachmentDownloadReport
	require.NoError(t, json.Unmarshal(get(path+"?limit=1", http.StatusOK).Body.Bytes(), &rows))
	require.Len(t, rows, 1)
	require.Equal(t, "started", rows[0].State)
	require.Equal(t, "excluded", rows[0].TransferState)
	cursor := strconv.FormatInt(rows[0].Sequence, 10)
	require.NoError(t, json.Unmarshal(get(path+"?limit=1&after="+cursor, http.StatusOK).Body.Bytes(), &rows))
	require.Len(t, rows, 1)
	require.Equal(t, "excluded", rows[0].State)
	get(path+"?limit=101", http.StatusBadRequest)
	get(path+"?after=-1", http.StatusBadRequest)
	get("/attachments/"+uuid.NewString()+"/download-history", http.StatusNotFound)
	w := request(http.MethodGet, ingestPath+"/receipts/"+event.EventUUID, token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var saved models.IngestReceipt
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.Equal(t, *original, saved)
	require.NoError(t, db.Close())
	// Export and restore use the standalone implementation, then a fresh native
	// open validates the relocated schema and recovers the original receipt.
	python, producerPath := nativeProducerRuntime(t)
	archivePath := filepath.Join(filepath.Dir(producerPath), "archive", "src")
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, "-c", attachmentDownloadRestore, db.DatabasePath(), t.TempDir())
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+archivePath)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	var restored struct {
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(output, &restored))
	_, err = sqlite.VerifyNativeSnapshot(t.Context(), restored.Path)
	require.NoError(t, err)
	require.NoError(t, db.Open(restored.Path))
	service = ingest.New(db.Repository())
	replayed, err := service.Receipt(t.Context(), token, event.EventUUID)
	require.NoError(t, err)
	require.Equal(t, original, replayed)
}

const attachmentDownloadRestore = `
from contextlib import closing
import json, sqlite3, sys
from pathlib import Path
from stash_archive.bundle import export_archive, import_archive
database, destination = map(Path, sys.argv[1:])
tables = {'source_attachment_downloads': 'id', 'ingest_receipts': 'producer_uuid,event_uuid',
          'source_runs': 'id', 'source_run_attempts': 'run_uuid,fence', 'source_attachments': 'uuid'}
def rows(path):
    with closing(sqlite3.connect(path.resolve().as_uri() + '?mode=ro', uri=True)) as db:
        return {t: db.execute(f'SELECT * FROM {t} ORDER BY {order}').fetchall() for t, order in tables.items()}
before = rows(database)
assert len(before['source_attachment_downloads']) == 2
export_archive(database, destination / 'archive', reserve=0)
import_archive(destination / 'archive', destination / 'relocated', reserve=0)
restored = destination / 'relocated' / 'library.sqlite'
assert rows(restored) == before
print(json.dumps({'path': str(restored)}))
`
