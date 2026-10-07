package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestArchiveActivityHTTPCompactStatusHistoryAndInvalidFilters(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "activity.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	now := time.Now().UTC().Truncate(time.Millisecond)
	var job *models.ArchiveJob
	var run *models.SourceRun
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		job, err = repo.ArchiveJob.Submit(ctx, models.ArchiveJobSubmission{RequestUUID: uuid.NewString(), Kind: models.ArchiveJobVerifyMedia,
			WorkKey: ingest.Digest([]byte("work")), ResourceKey: ingest.Digest([]byte("resource")),
			Arguments: json.RawMessage(`{"private":"not-in-activity"}`), MaxAttempts: 3}, now, 100)
		if err != nil {
			return err
		}
		job, err = repo.ArchiveJob.Claim(ctx, job.Kind, uuid.NewString(), now, time.Minute)
		if err != nil {
			return err
		}
		producer, err := repo.Ingest.CreateProducer(ctx, "Fixture")
		if err != nil {
			return err
		}
		collection, err := repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Original source", Kind: "feed", Namespace: "native:reddit", State: "active", TargetURL: "https://www.reddit.com/user/example/submitted/"}})
		if err != nil {
			return err
		}
		run, err = repo.SourceRun.Submit(ctx, producer.UUID, models.SourceRunRequest{RequestUUID: uuid.NewString(), CollectionUUID: collection.UUID,
			CollectionRevision: collection.Revision, Operation: "enrich", PolicySHA256: strings.Repeat("a", 64), Window: models.SourceWindow{Until: now}}, now, 100)
		return err
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	request := func(method, path string, expected int) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, expected, w.Code, w.Body.String())
		return w
	}
	jobPath, runPath := "/activity/jobs/"+job.UUID, "/activity/runs/"+run.UUID
	for _, path := range []string{"/activity/jobs", "/activity/jobs?kind=media.verify&state=running&limit=1", jobPath, jobPath + "/attempts"} {
		body := request(http.MethodGet, path, 200).Body.String()
		for _, excluded := range []string{"not-in-activity", "arguments", "work_key", "resource_key", "owner_uuid", "result"} {
			require.NotContains(t, body, excluded)
		}
	}
	var attempts []models.ArchiveActivityAttempt
	require.NoError(t, json.Unmarshal(request(http.MethodGet, jobPath+"/attempts", 200).Body.Bytes(), &attempts))
	require.Len(t, attempts, 1)
	require.Equal(t, "running", attempts[0].Outcome)
	require.Nil(t, attempts[0].EndedAt)
	var rows []models.SourceRunActivity
	require.NoError(t, json.Unmarshal(request(http.MethodGet, "/activity/runs?collection="+run.CollectionUUID, 200).Body.Bytes(), &rows))
	require.Len(t, rows, 1)
	require.Equal(t, "Original source", rows[0].CollectionLabel)
	require.Equal(t, "queued", rows[0].State)
	require.Equal(t, 1, rows[0].PendingWindows)
	var detail struct {
		Summary   models.SourceRunActivity `json:"summary"`
		Pending   []models.SourceWindow    `json:"pending"`
		Completed []models.SourceWindow    `json:"completed"`
	}
	body := request(http.MethodGet, runPath, 200).Body.Bytes()
	require.NoError(t, json.Unmarshal(body, &detail))
	require.Equal(t, rows[0], detail.Summary)
	require.Equal(t, run.Pending, detail.Pending)
	require.NotNil(t, detail.Completed)
	for _, excluded := range []string{"policy_sha256", "owner_uuid", "producer_uuid", "work_key"} {
		require.NotContains(t, string(body), excluded)
	}
	require.JSONEq(t, "[]", request(http.MethodGet, runPath+"/attempts", 200).Body.String())
	for _, path := range []string{
		"/activity/jobs?kind=nope", "/activity/jobs?state=deferred", "/activity/jobs?before=-1", "/activity/jobs?limit=101", "/activity/jobs?limit=0",
		"/activity/jobs?limit=1&limit=2", "/activity/jobs?limit=", "/activity/jobs?before=999999999999999999999999", "/activity/jobs?command=run",
		"/activity/runs?state=failed", "/activity/runs?collection=nope", "/activity/runs?limit=101", "/activity/runs?state=queued&state=running",
		jobPath + "/attempts?before=nope", runPath + "/attempts?limit=0", "/activity/jobs/not-a-uuid", "/activity/runs/not-a-uuid",
	} {
		require.Contains(t, request(http.MethodGet, path, 400).Body.String(), "invalid_activity_request")
	}
	for _, prefix := range []string{"/activity/jobs/", "/activity/runs/"} {
		path := prefix + uuid.NewString()
		request(http.MethodGet, path, 404)
		request(http.MethodGet, path+"/attempts", 404)
	}
	request(http.MethodPost, jobPath, 405)
	request(http.MethodPost, runPath, 405)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		currentJob, err := repo.ArchiveJob.Find(ctx, job.UUID)
		if err != nil {
			return err
		}
		require.Equal(t, job, currentJob)
		currentRun, err := repo.SourceRun.Find(ctx, run.UUID)
		require.Equal(t, run, currentRun)
		return err
	}))
}
