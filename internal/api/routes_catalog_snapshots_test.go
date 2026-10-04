package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonCatalogSnapshotUploadResumesLostResponses(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "native.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var mediaRoot *models.MediaRoot
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		mediaRoot, err = repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "migration", MediaRootDefinition: models.MediaRootDefinition{Label: "Historical catalog mount", State: "disabled"}})
		if err != nil {
			return err
		}
		_, _, err = db.ExecSQL(ctx, `INSERT INTO performers(id,created_at,updated_at) VALUES(71,'2020-01-01 00:00:00','2020-01-01 00:00:00'); INSERT INTO performer_names(performer_id,name,position) VALUES(71,'Selected name',0)`, nil)
		return err
	}))
	router := (&nativeArchiveRoutes{repo: repo}).router()
	handler := http.StripPrefix("/api/v3/archive", router)
	var lostBegin, lostChunk, lostEvidence, lostRelations, lostPublisher, lostAttachment atomic.Bool
	var lostMediaBegin, lostMediaAdvance, lostMembership, lostDocument, lostTranslation, lostEnrichment atomic.Bool
	var lostFileHistory atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		drop := recorder.Code == 200 && ((r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/catalog-snapshots") && !lostBegin.Swap(true)) ||
			(r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/chunks/0") && !lostChunk.Swap(true)) ||
			(r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/evidence-import") && !lostEvidence.Swap(true)) ||
			(r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/relations-import") && !lostRelations.Swap(true)) ||
			(r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/publisher-import") && !lostPublisher.Swap(true)) ||
			(r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/attachment-import") && !lostAttachment.Swap(true)) ||
			(r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/media-import") && !lostMediaBegin.Swap(true)) ||
			(r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/media-import/advance") && !lostMediaAdvance.Swap(true)) ||
			(r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/membership-import") && !lostMembership.Swap(true)) ||
			(r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/document-import") && !lostDocument.Swap(true)) ||
			(r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/translation-import") && !lostTranslation.Swap(true)) ||
			(r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/file-history-import") && !lostFileHistory.Swap(true)) ||
			(r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/enrichment-import") && !lostEnrichment.Swap(true)))
		if drop {
			if strings.HasSuffix(r.URL.Path, "/evidence-import") || strings.HasSuffix(r.URL.Path, "/relations-import") || strings.HasSuffix(r.URL.Path, "/publisher-import") || strings.HasSuffix(r.URL.Path, "/attachment-import") || strings.HasSuffix(r.URL.Path, "/media-import/advance") || strings.HasSuffix(r.URL.Path, "/membership-import") || strings.HasSuffix(r.URL.Path, "/document-import") || strings.HasSuffix(r.URL.Path, "/translation-import") || strings.HasSuffix(r.URL.Path, "/file-history-import") {
				var progress struct {
					State     string `json:"state"`
					Processed int    `json:"processed_records"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &progress); err != nil {
					t.Error(err)
				} else if progress.State != "running" || progress.Processed != 50 {
					t.Errorf("expected bounded first mapping transaction, got %+v", progress)
				}
			}
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
			return
		}
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	defer server.Close()
	snapshot := uuid.NewString()
	setup, err := json.Marshal(map[string]any{"directory": directory, "source": uuid.NewString(), "snapshot": snapshot, "endpoint": server.URL, "root_uuid": mediaRoot.UUID, "root_revision": mediaRoot.Revision})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_catalog_upload.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, lostBegin.Load())
	require.True(t, lostChunk.Load())
	require.True(t, lostEvidence.Load())
	require.True(t, lostRelations.Load())
	require.True(t, lostPublisher.Load())
	require.True(t, lostAttachment.Load())
	require.True(t, lostMediaBegin.Load())
	require.True(t, lostMediaAdvance.Load())
	require.True(t, lostMembership.Load())
	require.True(t, lostDocument.Load())
	require.True(t, lostTranslation.Load())
	require.True(t, lostEnrichment.Load())
	require.True(t, lostFileHistory.Load())
	var relationOrdinal, publisherOrdinal, attachmentOrdinal, mediaOrdinal, membershipOrdinal, documentOrdinal int64
	var translation models.CatalogTranslationRecord
	var enrichment models.CatalogEnrichmentRecord
	var fileHistory models.CatalogFileHistoryRecord
	var history *models.SourceFileHistory
	var membershipCollection, membershipPost string
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		receipt, err := repo.CatalogSnapshot.Find(ctx, snapshot)
		require.NoError(t, err)
		require.Equal(t, "received", receipt.State)
		require.False(t, receipt.Imported)
		performer, err := repo.Performer.Find(ctx, 71)
		require.NoError(t, err)
		require.Equal(t, "Selected name", performer.Name)
		relations, err := repo.CatalogRelationsImport.Records(ctx, snapshot, 0, 1)
		require.NoError(t, err)
		require.Len(t, relations, 1)
		relationOrdinal = relations[0].Ordinal
		publishers, err := repo.CatalogPublisherImport.Records(ctx, snapshot, 0, 1)
		require.NoError(t, err)
		require.Len(t, publishers, 1)
		publisherOrdinal = publishers[0].Ordinal
		attachments, err := repo.CatalogAttachmentImport.Records(ctx, snapshot, 0, 1)
		require.NoError(t, err)
		require.Len(t, attachments, 1)
		attachmentOrdinal = attachments[0].Ordinal
		media, err := repo.CatalogMediaImport.Records(ctx, snapshot, 0, 1)
		require.NoError(t, err)
		require.Len(t, media, 1)
		mediaOrdinal = media[0].Ordinal
		memberships, err := repo.CatalogMembershipImport.Records(ctx, snapshot, 0, 1)
		require.NoError(t, err)
		require.Len(t, memberships, 1)
		membershipOrdinal = memberships[0].Ordinal
		documents, err := repo.CatalogDocumentImport.Records(ctx, snapshot, 0, 1)
		require.NoError(t, err)
		require.Len(t, documents, 1)
		documentOrdinal = documents[0].Ordinal
		translations, err := repo.CatalogTranslationImport.Records(ctx, snapshot, 0, 1)
		require.NoError(t, err)
		require.Len(t, translations, 1)
		translation = translations[0]
		enrichments, err := repo.CatalogEnrichmentImport.Records(ctx, snapshot, 0, 1)
		require.NoError(t, err)
		require.Len(t, enrichments, 1)
		enrichment = enrichments[0]
		historyRows, err := repo.CatalogFileHistoryImport.Records(ctx, snapshot, 0, 1)
		require.NoError(t, err)
		require.Len(t, historyRows, 1)
		fileHistory = historyRows[0]
		history, err = repo.SourceFileHistory.Find(ctx, *fileHistory.HistoryUUID)
		require.NoError(t, err)
		require.NotNil(t, history.Locations[0].ObservationUUID)
		require.Nil(t, history.Locations[1].ObservationUUID)
		membershipCollection, membershipPost = *memberships[0].CollectionUUID, *memberships[0].PostUUID
		return nil
	}))
	for _, test := range []struct {
		method, path, contentType string
		status                    int
	}{
		{"GET", "/catalog-snapshots/" + snapshot + "/file-history-import", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/file-history-import/records?limit=1", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/file-history-import/records?limit=101", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/file-history-import/records?after=-1", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/file-history-import/records/" + strconv.FormatInt(fileHistory.Ordinal, 10), "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/file-history-import/records/999999", "", 404},
		{"GET", "/catalog-snapshots/" + uuid.NewString() + "/file-history-import", "", 404},
		{"POST", "/catalog-snapshots/" + snapshot + "/file-history-import", "application/json", 400},
		{"GET", "/file-history/" + history.UUID, "", 200},
		{"GET", "/file-history/invalid", "", 400},
		{"GET", "/file-history/" + uuid.NewString(), "", 404},
		{"GET", "/file-observations/" + *history.Locations[0].ObservationUUID + "/history?limit=1", "", 200},
		{"GET", "/file-observations/" + *history.Locations[0].ObservationUUID + "/history?limit=101", "", 400},
		{"GET", "/file-observations/" + *history.Locations[0].ObservationUUID + "/history?after=invalid", "", 400},
		{"GET", "/file-observations/" + uuid.NewString() + "/history", "", 404},
		{"GET", "/file-observations/invalid/history", "", 400},
		{"GET", "/content-claims/" + history.Deduplication.ContentClaimUUID + "/history", "", 200},
		{"GET", "/content-claims/" + history.Deduplication.ContentClaimUUID + "/history?limit=-1", "", 400},
		{"GET", "/content-claims/invalid/history", "", 400},
		{"GET", "/content-claims/" + uuid.NewString() + "/history", "", 404},
		{"GET", "/catalog-snapshots/" + snapshot + "/enrichment-import", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/enrichment-import/records?limit=1", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/enrichment-import/records?limit=101", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/enrichment-import/records?after=-1", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/enrichment-import/records/" + strconv.FormatInt(enrichment.Ordinal, 10), "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/enrichment-import/records/0", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/enrichment-import/records/999999", "", 404},
		{"GET", "/catalog-snapshots/" + uuid.NewString() + "/enrichment-import", "", 404},
		{"GET", "/catalog-snapshots/" + uuid.NewString() + "/enrichment-import/records", "", 404},
		{"POST", "/catalog-snapshots/" + snapshot + "/enrichment-import", "application/json", 400},
		{"POST", "/catalog-snapshots/" + snapshot + "/enrichment-import", "text/plain", 400},
		{"GET", "/enrichment-receipts/" + *enrichment.ReceiptUUID, "", 200},
		{"GET", "/enrichment-receipts/invalid", "", 400},
		{"GET", "/enrichment-receipts/" + uuid.NewString(), "", 404},
		{"GET", "/posts/" + *enrichment.PostUUID + "/enrichment-receipts?limit=1", "", 200},
		{"GET", "/posts/" + *enrichment.PostUUID + "/enrichment-receipts?limit=101", "", 400},
		{"GET", "/posts/" + *enrichment.PostUUID + "/enrichment-receipts?after=invalid", "", 400},
		{"GET", "/posts/" + uuid.NewString() + "/enrichment-receipts", "", 404},
		{"GET", "/posts/invalid/enrichment-receipts", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/translation-import", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/translation-import/records?limit=1", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/translation-import/records?limit=101", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/translation-import/records?after=-1", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/translation-import/records/" + strconv.FormatInt(translation.Ordinal, 10), "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/translation-import/records/0", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/translation-import/records/999999", "", 404},
		{"GET", "/catalog-snapshots/" + uuid.NewString() + "/translation-import", "", 404},
		{"GET", "/catalog-snapshots/" + uuid.NewString() + "/translation-import/records", "", 404},
		{"POST", "/catalog-snapshots/" + snapshot + "/translation-import", "application/json", 400},
		{"POST", "/catalog-snapshots/" + snapshot + "/translation-import", "text/plain", 400},
		{"GET", "/translations/" + *translation.TranslationUUID, "", 200},
		{"GET", "/translation-evidence/" + *translation.EvidenceUUID, "", 200},
		{"GET", "/translations/invalid", "", 400},
		{"GET", "/translation-evidence/invalid", "", 400},
		{"GET", "/translations/" + uuid.NewString(), "", 404},
		{"GET", "/translation-evidence/" + uuid.NewString(), "", 404},
		{"GET", "/posts/" + *translation.PostUUID + "/translations?limit=1", "", 200},
		{"GET", "/posts/" + *translation.PostUUID + "/translations?limit=101", "", 400},
		{"GET", "/posts/" + *translation.PostUUID + "/translations?after=invalid", "", 400},
		{"GET", "/posts/" + *translation.PostUUID + "/translations?original_sha256=invalid", "", 400},
		{"GET", "/posts/" + uuid.NewString() + "/translations", "", 404},
		{"GET", "/posts/invalid/translations", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot, "", 200},
		{"GET", "/catalog-snapshots/not-a-uuid", "", 400},
		{"GET", "/catalog-snapshots/" + uuid.NewString(), "", 404},
		{"POST", "/catalog-snapshots", "application/json", 400},
		{"POST", "/catalog-snapshots", "text/plain", 400},
		{"PUT", "/catalog-snapshots/" + snapshot + "/chunks/-1", "application/x-ndjson", 400},
		{"PUT", "/catalog-snapshots/" + snapshot + "/chunks/0", "application/json", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/evidence-import", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/evidence-import/records?limit=1", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/evidence-import/records?limit=101", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/evidence-import/records?after=-1", "", 400},
		{"POST", "/catalog-snapshots/" + snapshot + "/evidence-import", "application/json", 400},
		{"POST", "/catalog-snapshots/" + snapshot + "/evidence-import", "text/plain", 400},
		{"GET", "/collections/" + membershipCollection + "/post-memberships?limit=1", "", 200},
		{"GET", "/posts/" + membershipPost + "/collection-memberships?limit=1", "", 200},
		{"GET", "/posts/" + membershipPost + "/collection-memberships?limit=101", "", 400},
		{"GET", "/collections/" + membershipCollection + "/post-memberships?after=invalid", "", 400},
		{"GET", "/collections/" + uuid.NewString() + "/post-memberships", "", 404},
		{"GET", "/posts/" + uuid.NewString() + "/collection-memberships", "", 404},
		{"GET", "/catalog-snapshots/" + snapshot + "/document-import", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/document-import/records?limit=1", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/document-import/records?limit=101", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/document-import/records?after=-1", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/document-import/records/" + strconv.FormatInt(documentOrdinal, 10), "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/document-import/records/0", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/document-import/records/999999", "", 404},
		{"POST", "/catalog-snapshots/" + snapshot + "/document-import", "application/json", 400},
		{"GET", "/catalog-snapshots/" + uuid.NewString() + "/document-import", "", 404},
		{"GET", "/catalog-snapshots/" + uuid.NewString() + "/document-import/records", "", 404},
		{"GET", "/catalog-snapshots/" + snapshot + "/membership-import", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/membership-import/records?limit=1", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/membership-import/records?limit=101", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/membership-import/records?after=-1", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/membership-import/records/" + strconv.FormatInt(membershipOrdinal, 10), "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/membership-import/records/0", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/membership-import/records/999999", "", 404},
		{"POST", "/catalog-snapshots/" + snapshot + "/membership-import", "application/json", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/relations-import", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/relations-import/records?limit=1", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/relations-import/records?limit=101", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/relations-import/records?after=-1", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/relations-import/records/" + strconv.FormatInt(relationOrdinal, 10), "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/relations-import/records/0", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/relations-import/records/not-an-ordinal", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/relations-import/records/999999", "", 404},
		{"POST", "/catalog-snapshots/" + snapshot + "/relations-import", "application/json", 400},
		{"POST", "/catalog-snapshots/" + snapshot + "/relations-import", "text/plain", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/publisher-import", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/publisher-import/records?limit=1", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/publisher-import/records?limit=101", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/publisher-import/records?after=-1", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/publisher-import/records/" + strconv.FormatInt(publisherOrdinal, 10), "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/publisher-import/records/0", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/publisher-import/records/not-an-ordinal", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/publisher-import/records/999999", "", 404},
		{"POST", "/catalog-snapshots/" + snapshot + "/publisher-import", "application/json", 400},
		{"POST", "/catalog-snapshots/" + snapshot + "/publisher-import", "text/plain", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/attachment-import", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/attachment-import/records?limit=1", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/attachment-import/records?limit=101", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/attachment-import/records?after=-1", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/attachment-import/records/" + strconv.FormatInt(attachmentOrdinal, 10), "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/attachment-import/records/0", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/attachment-import/records/not-an-ordinal", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/attachment-import/records/999999", "", 404},
		{"POST", "/catalog-snapshots/" + snapshot + "/attachment-import", "application/json", 400},
		{"POST", "/catalog-snapshots/" + snapshot + "/attachment-import", "text/plain", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/media-import", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/media-import/records?limit=1", "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/media-import/records?limit=101", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/media-import/records?after=-1", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/media-import/records/" + strconv.FormatInt(mediaOrdinal, 10), "", 200},
		{"GET", "/catalog-snapshots/" + snapshot + "/media-import/records/0", "", 400},
		{"GET", "/catalog-snapshots/" + snapshot + "/media-import/records/999999", "", 404},
		{"POST", "/catalog-snapshots/" + snapshot + "/media-import", "application/json", 400},
		{"POST", "/catalog-snapshots/" + snapshot + "/media-import", "text/plain", 400},
		{"POST", "/catalog-snapshots/" + snapshot + "/media-import/advance", "application/json", 400},
	} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader("{}"))
		request.Header.Set("Content-Type", test.contentType)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, test.status, response.Code, "%s %s: %s", test.method, test.path, response.Body.String())
		if test.status == 200 {
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		}
	}
	for _, test := range []struct {
		query string
		count int
	}{{"?target_language=en", 2}, {"?target_language=ja", 0}, {"?target_language=", 0}, {"?limit=1", 1}} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/posts/"+*translation.PostUUID+"/translations"+test.query, nil))
		require.Equal(t, 200, response.Code)
		var evidence []models.SourceTranslationEvidence
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &evidence))
		require.Len(t, evidence, test.count, test.query)
		require.NotContains(t, response.Body.String(), "translated_text")
	}
	// Reopening exercises persisted importer/result/provenance reconciliation,
	// including literal Unicode separators and a NUL in the source text.
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(filepath.Join(directory, "native.sqlite")))
	request := httptest.NewRequest("POST", "/catalog-snapshots", strings.NewReader("{}"))
	request.Header.Set("Origin", "https://untrusted.example")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, 403, response.Code)
}
