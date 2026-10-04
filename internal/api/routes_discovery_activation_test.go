package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryActivationHTTPRecoversAndInspectsSharedCandidateEvidence(t *testing.T) {
	discoveryActivationHTTP(t, false)
}

func TestDiscoveryPublicationHTTPRecoversLostResponseAndRejectsImplicitPostMerge(t *testing.T) {
	discoveryActivationHTTP(t, true)
}

func TestDiscoveryRecoveryHTTPUsesSavedPlanAndPublishesFreshCoverage(t *testing.T) {
	discoveryActivationRecoveryHTTP(t, false, true)
}

func discoveryActivationHTTP(t *testing.T, fresh bool) {
	t.Helper()
	discoveryActivationRecoveryHTTP(t, fresh, false)
}

func discoveryActivationRecoveryHTTP(t *testing.T, fresh, recovery bool) {
	discoveryActivationScenario(t, fresh, recovery, false)
}

func TestPythonDiscoveryDetailExecutionRecoversOriginalEvidence(t *testing.T) {
	discoveryActivationScenario(t, false, false, true)
}

func discoveryActivationScenario(t *testing.T, fresh, recovery, pythonDetails bool) {
	t.Helper()
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "native.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	source := uuid.NewString()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if _, _, err := db.ExecSQL(ctx, "INSERT INTO performers(id,created_at,updated_at) VALUES(71,'2020-01-01 00:00:00','2020-01-01 00:00:00'); INSERT INTO performer_names(performer_id,name,position) VALUES(71,'Preserved name',0)", nil); err != nil {
			return err
		}
		for i := range 2 {
			ref, err := scrape.CatalogLocalPostReference(source, "c_"+strings.Repeat("1", 32), fmt.Sprintf("reddit:post:activation%03d", i))
			if err != nil {
				return err
			}
			if _, err := repo.SourceEvidence.EnsurePost(ctx, ref, ""); err != nil {
				return err
			}
		}
		return nil
	}))
	router := (&nativeArchiveRoutes{repo: repo}).router()
	handler := http.StripPrefix("/api/v3/archive", router)
	var lost atomic.Bool
	var lostRecovery atomic.Bool
	var lostPublication atomic.Bool
	var applies atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		if recorder.Code == http.StatusOK && r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/discovery-activations") {
			applies.Add(1)
			var receipt models.DiscoveryActivation
			if err := json.Unmarshal(recorder.Body.Bytes(), &receipt); err != nil {
				t.Error(err)
				return
			}
			lose := !lost.Swap(true)
			if receipt.Input.Listing.RecoveryOf != nil {
				lose = !lostRecovery.Swap(true)
			}
			if lose {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = connection.Close()
				return
			}
		}
		if recorder.Code == http.StatusOK && r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/publication") && !lostPublication.Swap(true) {
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
	setup, err := json.Marshal(map[string]any{"directory": directory, "source": source, "snapshot": uuid.NewString(), "endpoint": server.URL, "fresh": fresh, "recovery": recovery})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_discovery_activation.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, lost.Load())
	if recovery {
		require.EqualValues(t, 4, applies.Load())
		require.True(t, lostRecovery.Load())
		fresh = true
	} else {
		require.EqualValues(t, 2, applies.Load())
	}
	var receipt models.DiscoveryActivation
	require.NoError(t, json.Unmarshal(output, &receipt))
	require.Len(t, receipt.Entries, 2)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		job, err := repo.DiscoveryJob.Job(ctx, receipt.Input.Listing.UUID)
		require.NoError(t, err)
		require.Nil(t, job, "review activation did not admit a source job")
		return nil
	}))
	request := func(method, path string, input any, status int) []byte {
		body, err := json.Marshal(input)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		return w.Body.Bytes()
	}
	for _, path := range []string{"/discovery-match-targets/bad", "/discovery-match-targets/bad/review", "/discovery-listings/bad", "/discovery-activations/bad", "/discovery-match-candidates/0/evidence",
		"/discovery-match-candidates/1/evidence?after=10001", "/discovery-match-targets/" + receipt.Entries[0].TargetUUID + "/candidates?limit=101"} {
		request("GET", path, nil, http.StatusBadRequest)
	}
	for _, path := range []string{"/discovery-activations/", "/discovery-match-targets/", "/discovery-listings/"} {
		request("GET", path+uuid.NewString(), nil, http.StatusNotFound)
	}
	request("GET", "/discovery-match-targets/"+uuid.NewString()+"/review", nil, http.StatusNotFound)
	initialReview := enrichmentHTTPValue[models.DiscoveryMatchReview](t, request("GET", "/discovery-match-targets/"+receipt.Entries[0].TargetUUID+"/review", nil, 200))
	historical, initialBlockers := int64(67), []string{"history_not_retained", "listing_incomplete"}
	if fresh {
		historical, initialBlockers = 0, []string{"listing_incomplete"}
	}
	require.Equal(t, historical, initialReview.Coverage.HistoricalPages)
	require.Equal(t, !fresh, initialReview.Coverage.StartsAtSavedCursor)
	require.False(t, initialReview.Coverage.Complete)
	require.Equal(t, initialBlockers, initialReview.Blockers)
	publicationPath := "/discovery-match-targets/" + receipt.Entries[0].TargetUUID + "/publication"
	request("GET", publicationPath, nil, http.StatusNotFound)
	request("POST", publicationPath, map[string]any{"expected_target_revision": 2}, http.StatusConflict)
	request("POST", "/discovery-activations", map[string]any{"input": receipt.Input, "expected_plan_sha256": strings.Repeat("f", 64)}, http.StatusConflict)
	csrf := httptest.NewRequest("POST", "/discovery-activations", bytes.NewBufferString(`{}`))
	csrf.Header.Set("Origin", "https://unrelated.invalid")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, csrf)
	require.Equal(t, http.StatusForbidden, w.Code)

	service := ingest.New(repo)
	var producer *models.IngestProducer
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		producer, err = repo.Ingest.CreateProducer(ctx, "Fixture discovery producer")
		return err
	}))
	_, token, err := service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: receipt.Input.Listing.CollectionUUID}}, nil)
	require.NoError(t, err)
	producerHandler := withIngestRoutes(http.NotFoundHandler(), service, false)
	for _, path := range []string{"/discovery-activations", "/discovery-activations/preview", "/discovery-activations/" + receipt.Input.UUID, "/discovery-match-targets/" + receipt.Entries[0].TargetUUID, "/discovery-match-targets/" + receipt.Entries[0].TargetUUID + "/detail-preview", "/discovery-listings/" + receipt.Input.Listing.UUID, publicationPath} {
		r := httptest.NewRequest("POST", "/api/v3/archive"+path, bytes.NewBufferString(`{}`))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		producerHandler.ServeHTTP(w, r)
		require.Equal(t, http.StatusNotFound, w.Code)
	}
	r := httptest.NewRequest("GET", "/api/v3/archive/discovery-match-targets/"+receipt.Entries[0].TargetUUID+"/review", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	producerHandler.ServeHTTP(w, r)
	require.Equal(t, http.StatusNotFound, w.Code, "producer credentials cannot inspect application reviews")
	coordinator := ingest.NewDiscoveryCoordinator(service)
	listing := receipt.Input.Listing
	job, err := coordinator.Admit(t.Context(), token, listing.UUID, receipt.ListingSHA256, listing.PolicySHA256, listing.ExtractorVersion)
	require.NoError(t, err)
	running, err := coordinator.Claim(t.Context(), token, job.UUID, job.Revision, uuid.NewString(), listing.PolicySHA256, listing.ExtractorVersion, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, running)
	body, err := os.ReadFile("../../pkg/archive/testdata/discovery-pages-v1.json")
	require.NoError(t, err)
	var corpus struct {
		Pages []struct {
			Page json.RawMessage `json:"page"`
		} `json:"pages"`
	}
	require.NoError(t, json.Unmarshal(body, &corpus))
	page, err := archive.DecodeJSONObject(corpus.Pages[1].Page, archive.MaxDiscoveryPageBytes)
	require.NoError(t, err)
	page["url"], page["cursor"], page["complete"], page["next_cursor"] = listing.ProfileURL, listing.InitialCursor, true, nil
	patch := page["records"].([]any)[0].(map[string]any)["patch"].(map[string]any)
	patch["source_extractor_url"], patch["author"] = listing.ProfileURL, "deliberately-unlinked"
	if fresh {
		patch["date"] = "2026-10-03"
	}
	body, err = archive.EncodeSourceJSON(page)
	require.NoError(t, err)
	_, err = coordinator.AppendPage(t.Context(), token, running.Lease(), 1, body)
	require.NoError(t, err)
	worker := ingest.NewDiscoveryComparisonWorker(service)
	for range 2 {
		result, err := worker.Process(t.Context())
		require.NoError(t, err)
		require.NotNil(t, result.Receipt)
		require.True(t, result.Receipt.Complete)
	}
	for _, entry := range receipt.Entries {
		path := "/discovery-match-targets/" + entry.TargetUUID
		review := enrichmentHTTPValue[models.DiscoveryMatchReview](t, request("GET", path+"/review", nil, 200))
		require.Equal(t, historical, review.Coverage.HistoricalPages)
		require.Equal(t, 1, review.Coverage.RetainedPages)
		require.True(t, review.Coverage.RetainedComplete)
		require.True(t, review.Target.EnumerationComplete)
		require.Equal(t, fresh, review.Coverage.Complete, "resumed enumeration cannot prove missing historical coverage")
		if fresh {
			require.Empty(t, review.Blockers)
			require.Zero(t, review.DetailCandidateCount)
		} else {
			require.Equal(t, []string{"history_not_retained", "detail_required"}, review.Blockers)
			require.Equal(t, 1, review.DetailCandidateCount)
		}
		require.Equal(t, 1, review.CandidateCount)
		candidates := enrichmentHTTPValue[[]models.DiscoveryMatchCandidate](t, request("GET", path+"/candidates", nil, 200))
		require.Len(t, candidates, 1, "multiple attachments produce one candidate per target")
		evidence := enrichmentHTTPValue[[]models.DiscoveryMatchEvidence](t, request("GET", "/discovery-match-candidates/"+strconv.FormatInt(candidates[0].Sequence, 10)+"/evidence", nil, 200))
		require.Len(t, evidence, 1)
		require.Equal(t, []int{0, 1, 2}, evidence[0].RecordOrdinals)
		require.Equal(t, !fresh, evidence[0].NeedsDetail, "a title-only match cannot establish an accepted identity")
		if !fresh {
			detailPage, err := archive.DecodeJSONObject(body, archive.MaxDiscoveryPageBytes)
			require.NoError(t, err)
			for _, record := range detailPage["records"].([]any) {
				patch := record.(map[string]any)["patch"].(map[string]any)
				if _, exists := patch["source_extractor_url"]; exists {
					patch["source_extractor_url"] = candidates[0].URL
				}
			}
			detailPage["records"].([]any)[0].(map[string]any)["patch"].(map[string]any)["date"] = "2026-10-03"
			detailBody, err := archive.EncodeSourceJSON(map[string]any{"schema": archive.EnrichmentTranscriptSchema,
				"url": candidates[0].URL, "extractor_version": listing.ExtractorVersion, "retention_policy": archive.SourceRetentionVersion,
				"records": detailPage["records"], "pending": []any{}, "unresolved": []any{}})
			require.NoError(t, err)
			input := models.DiscoveryDetailPreviewInput{ExpectedTargetRevision: review.Target.Revision, CandidateSequence: candidates[0].Sequence,
				ExtractorVersion: listing.ExtractorVersion, Body: detailBody}
			preview := enrichmentHTTPValue[models.DiscoveryDetailPreview](t, request("POST", path+"/detail-preview", input, http.StatusOK))
			require.True(t, preview.PreviewOnly)
			require.Equal(t, "corroborated", preview.Evidence.Status)
			require.Equal(t, review.Blockers, preview.Blockers, "preview does not clear missing history or detail_required")
			require.Equal(t, review, enrichmentHTTPValue[models.DiscoveryMatchReview](t, request("GET", path+"/review", nil, http.StatusOK)))
			request("POST", publicationPath, map[string]any{"expected_target_revision": 2}, http.StatusConflict)
			if pythonDetails {
				exercisePythonDiscoveryDetailHTTP(t, service, token, review, detailBody)
			} else {
				exerciseDiscoveryDetailHTTP(t, service, producerHandler, token, review, listing, detailBody)
			}
			input.ExpectedTargetRevision++
			request("POST", path+"/detail-preview", input, http.StatusConflict)
			input.ExpectedTargetRevision--
			input.Body = json.RawMessage(`"escaped transcript"`)
			request("POST", path+"/detail-preview", input, http.StatusBadRequest)
			input.Body = detailBody
			request("POST", "/discovery-match-targets/"+uuid.NewString()+"/detail-preview", input, http.StatusNotFound)
			for _, invalid := range []string{`{"expected_target_revision":2,"expected_target_revision":2}`, `{"settings":{}}`} {
				r := httptest.NewRequest("POST", path+"/detail-preview", strings.NewReader(invalid))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				require.Equal(t, http.StatusBadRequest, w.Code)
			}
		}
	}
	pages := enrichmentHTTPValue[[]models.DiscoveryPageReceipt](t, request("GET", "/discovery-listings/"+listing.UUID+"/pages", nil, 200))
	require.Len(t, pages, 1, "both targets share the original stored page")
	var publication *models.DiscoveryMatchPublication
	if fresh {
		postBody := bytes.NewBufferString(`{"expected_target_revision":2}`)
		r, err := http.NewRequestWithContext(t.Context(), "POST", server.URL+"/api/v3/archive"+publicationPath, postBody)
		require.NoError(t, err)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("ApiKey", "fixture-application-key")
		response, err := server.Client().Do(r)
		if response != nil {
			_ = response.Body.Close()
		}
		require.Error(t, err, "the committed publication response was deliberately lost")
		require.True(t, lostPublication.Load())
		value := enrichmentHTTPValue[models.DiscoveryMatchPublication](t, request("GET", publicationPath, nil, 200))
		publication = &value
		require.Equal(t, receipt.Entries[0].PostUUID, publication.PostUUID)
		require.Equal(t, 3, publication.RecordCount)
		replay := enrichmentHTTPValue[models.DiscoveryMatchPublication](t, request("POST", publicationPath, map[string]any{"expected_target_revision": 2}, 200))
		require.Equal(t, *publication, replay)
		records := enrichmentHTTPValue[[]models.DiscoveryPublishedRecord](t, request("GET", publicationPath+"/records?limit=1", nil, 200))
		require.Len(t, records, 1)
		require.Zero(t, records[0].Ordinal)
		rest := enrichmentHTTPValue[[]models.DiscoveryPublishedRecord](t, request("GET", publicationPath+"/records?after=0", nil, 200))
		require.Len(t, rest, 2)
		request("GET", publicationPath+"/records?after=-2", nil, 400)
		request("GET", publicationPath+"/records?limit=101", nil, 400)
		request("POST", publicationPath, map[string]any{"expected_target_revision": 3}, 409)
		request("POST", "/discovery-match-targets/"+receipt.Entries[1].TargetUUID+"/publication", map[string]any{"expected_target_revision": 2}, 409)
		require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			captures, err := repo.SourceEvidence.Captures(ctx, publication.PostUUID, nil, 100)
			require.NoError(t, err)
			require.Len(t, captures, 3)
			for _, capture := range captures {
				preview, err := repo.CapturePublisher.Preview(ctx, capture.UUID, "")
				require.NoError(t, err)
				require.NotEqual(t, "unavailable", preview.Action, "publication uses the shared publisher domain service")
				manifest, err := repo.SourceAttachment.ManifestForCapture(ctx, capture.UUID)
				require.NoError(t, err)
				require.NotNil(t, manifest, "album observations use the shared attachment domain service")
			}
			selection, err := repo.SourceAttachment.Selection(ctx, publication.PostUUID)
			require.NoError(t, err)
			require.NotNil(t, selection)
			return nil
		}))
	} else {
		request("POST", publicationPath, map[string]any{"expected_target_revision": 2}, 409)
	}
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	replayed := enrichmentHTTPValue[models.DiscoveryActivation](t, request("GET", "/discovery-activations/"+receipt.Input.UUID, nil, 200))
	require.Equal(t, receipt, replayed)
	if publication != nil {
		value := enrichmentHTTPValue[models.DiscoveryMatchPublication](t, request("POST", publicationPath, map[string]any{"expected_target_revision": 2}, 200))
		require.Equal(t, *publication, value, "publication receipt survives reopening")
	}
}
