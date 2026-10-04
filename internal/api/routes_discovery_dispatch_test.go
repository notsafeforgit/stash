package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryWorkerHTTPReadinessUsesCurrentScopeAndBoundedCursors(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	prefix := "/discovery/collections/" + f.collection.UUID
	input := map[string]any{"policy_sha256": f.listing.PolicySHA256, "extractor_version": f.listing.ExtractorVersion, "limit": 1}
	for _, suffix := range []string{"/jobs/ready", "/listings/ready"} {
		body := &enrichmentUnreadBody{}
		r := httptest.NewRequest("POST", ingestPath+prefix+suffix, body)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		require.Equal(t, 401, w.Code)
		require.False(t, body.read)
		f.request(t, "POST", "/discovery/collections/invalid"+suffix, input, 400)
		f.request(t, "POST", "/discovery/collections/"+uuid.NewString()+suffix, input, 404)
		input["limit"] = 101
		f.request(t, "POST", prefix+suffix, input, 400)
		input["limit"] = 1
		input["policy_sha256"] = "invalid"
		f.request(t, "POST", prefix+suffix, input, 400)
		input["policy_sha256"] = f.listing.PolicySHA256
	}
	caps := enrichmentHTTPValue[map[string]any](t, f.request(t, http.MethodGet, "/capabilities", nil, 200))
	require.EqualValues(t, 1, caps["discovery_readiness_protocol"])
	page := enrichmentHTTPValue[models.DiscoveryListingCandidates](t, f.request(t, "POST", prefix+"/listings/ready", input, 200))
	require.Equal(t, []models.DiscoveryListingCandidate{{UUID: f.listing.UUID, Digest: f.listing.Digest}}, page.Listings)
	require.Equal(t, f.listing.UUID, page.After)
	require.False(t, page.HasMore)
	input["after"] = page.After
	page = enrichmentHTTPValue[models.DiscoveryListingCandidates](t, f.request(t, "POST", prefix+"/listings/ready", input, 200))
	require.Empty(t, page.Listings)
	require.Equal(t, f.listing.UUID, page.After)
	input["after"] = 0
	f.request(t, "POST", prefix+"/listings/ready", input, 400)
	job := f.admit(t)
	ready := enrichmentHTTPValue[[]models.DiscoveryJobCandidate](t, f.request(t, "POST", prefix+"/jobs/ready", input, 200))
	require.Equal(t, []models.DiscoveryJobCandidate{{UUID: job.UUID, Sequence: job.Sequence}}, ready)
	input["after"] = job.Sequence
	require.Empty(t, enrichmentHTTPValue[[]models.DiscoveryJobCandidate](t, f.request(t, "POST", prefix+"/jobs/ready", input, 200)))
	input["after"] = -1
	f.request(t, "POST", prefix+"/jobs/ready", input, 400)
	input["after"] = ""
	f.request(t, "POST", prefix+"/jobs/ready", input, 400)
	delete(input, "after")
	input["policy_sha256"] = strings.Repeat("b", 64)
	require.Empty(t, enrichmentHTTPValue[[]models.DiscoveryJobCandidate](t, f.request(t, "POST", prefix+"/jobs/ready", input, 200)))
	input["policy_sha256"] = f.listing.PolicySHA256
	var other *models.SourceCollection
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		other, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Other", Kind: "feed", Namespace: "native:reddit", State: "active"}})
		return err
	}))
	_, token, err := f.service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: other.UUID}}, nil)
	require.NoError(t, err)
	f.token = token
	f.request(t, "POST", prefix+"/jobs/ready", input, 403)
	f.request(t, "POST", prefix+"/listings/ready", input, 403)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := f.repo.DiscoveryJob.Job(ctx, f.listing.UUID)
		require.NoError(t, err)
		require.Equal(t, job, *current, "readiness neither claims nor changes the admitted job")
		return nil
	}))
}

func TestDiscoveryMaintenanceRuntimeRecoversWithoutProducer(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	job := f.admit(t)
	running := f.claim(t, job, uuid.NewString())
	f.now = running.LeaseUntil.Add(time.Second)
	worker := ingest.NewDiscoveryMaintenance(f.service)
	worker.Now = func() time.Time { return f.now }
	worker.PollInterval = time.Millisecond
	runtime := &archiveWorkerRuntime{worker: worker}
	t.Cleanup(runtime.stop)
	runtime.start()
	runtime.start()
	require.Eventually(t, func() bool {
		var current *models.ArchiveJob
		err := f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			current, err = f.repo.ArchiveJob.Find(ctx, job.UUID)
			return err
		})
		return err == nil && current.State == "queued"
	}, 5*time.Second, 10*time.Millisecond)
	(&Server{discoveryMaintenance: runtime}).Shutdown()
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := f.repo.ArchiveJob.Find(ctx, job.UUID)
		require.NoError(t, err)
		require.Equal(t, f.now.Add(5*time.Minute), current.AvailableAt)
		attempts, err := f.repo.ArchiveJob.Attempts(ctx, job.UUID, 0, 10)
		require.NoError(t, err)
		require.Len(t, attempts, 1)
		require.Equal(t, "expired", attempts[0].Outcome)
		return nil
	}))
}
