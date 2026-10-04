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
			if !lost.Swap(true) {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = connection.Close()
				return
			}
		}
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	defer server.Close()
	setup, err := json.Marshal(map[string]any{"directory": directory, "source": source, "snapshot": uuid.NewString(), "endpoint": server.URL})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_discovery_activation.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, lost.Load())
	require.EqualValues(t, 2, applies.Load())
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
	for _, path := range []string{"/discovery-match-targets/bad", "/discovery-listings/bad", "/discovery-activations/bad", "/discovery-match-candidates/0/evidence",
		"/discovery-match-candidates/1/evidence?after=10001", "/discovery-match-targets/" + receipt.Entries[0].TargetUUID + "/candidates?limit=101"} {
		request("GET", path, nil, http.StatusBadRequest)
	}
	for _, path := range []string{"/discovery-activations/", "/discovery-match-targets/", "/discovery-listings/"} {
		request("GET", path+uuid.NewString(), nil, http.StatusNotFound)
	}
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
	for _, path := range []string{"/discovery-activations", "/discovery-activations/preview", "/discovery-activations/" + receipt.Input.UUID, "/discovery-match-targets/" + receipt.Entries[0].TargetUUID, "/discovery-listings/" + receipt.Input.Listing.UUID} {
		r := httptest.NewRequest("POST", "/api/v3/archive"+path, bytes.NewBufferString(`{}`))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		producerHandler.ServeHTTP(w, r)
		require.Equal(t, http.StatusNotFound, w.Code)
	}
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
		candidates := enrichmentHTTPValue[[]models.DiscoveryMatchCandidate](t, request("GET", path+"/candidates", nil, 200))
		require.Len(t, candidates, 1, "multiple attachments produce one candidate per target")
		evidence := enrichmentHTTPValue[[]models.DiscoveryMatchEvidence](t, request("GET", "/discovery-match-candidates/"+strconv.FormatInt(candidates[0].Sequence, 10)+"/evidence", nil, 200))
		require.Len(t, evidence, 1)
		require.Equal(t, []int{0, 1, 2}, evidence[0].RecordOrdinals)
		require.True(t, evidence[0].NeedsDetail, "a retained cursor and title-only match cannot establish an accepted identity")
	}
	pages := enrichmentHTTPValue[[]models.DiscoveryPageReceipt](t, request("GET", "/discovery-listings/"+listing.UUID+"/pages", nil, 200))
	require.Len(t, pages, 1, "both targets share the original stored page")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	replayed := enrichmentHTTPValue[models.DiscoveryActivation](t, request("GET", "/discovery-activations/"+receipt.Input.UUID, nil, 200))
	require.Equal(t, receipt, replayed)
}
