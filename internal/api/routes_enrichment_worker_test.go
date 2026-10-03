package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type enrichmentHTTPFixture struct {
	repo       models.Repository
	service    *ingest.Service
	worker     *ingest.EnrichmentCoordinator
	handler    http.Handler
	collection *models.SourceCollection
	target     *models.EnrichmentTarget
	producer   *models.IngestProducer
	token      string
	now        time.Time
	initial    json.RawMessage
	complete   json.RawMessage
}

func newEnrichmentHTTPFixture(t *testing.T) *enrichmentHTTPFixture {
	t.Helper()
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "enrichment.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	f := &enrichmentHTTPFixture{repo: db.Repository(), now: time.Now().UTC().Truncate(time.Millisecond)}
	f.service = ingest.New(f.repo)
	f.worker = ingest.NewEnrichmentCoordinator(f.service)
	f.worker.Now = func() time.Time { return f.now }
	f.handler = (&ingestRoutes{service: f.service, enrichment: f.worker}).router()
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		post, err := f.repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, "")
		if err != nil {
			return err
		}
		url, err := f.repo.SourcePostLinks.ObserveURL(ctx, models.SourcePostURLInput{SourcePostEvidence: models.SourcePostEvidence{
			UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "review", Basis: "source_post", ObservedAt: f.now}, URL: "https://www.reddit.com/comments/abc123"})
		if err != nil {
			return err
		}
		f.collection, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Source feed", Kind: "feed", Namespace: "native:reddit", State: "active"}})
		if err != nil {
			return err
		}
		f.producer, err = f.repo.Ingest.CreateProducer(ctx, "Metadata worker")
		if err != nil {
			return err
		}
		f.target, err = f.repo.EnrichmentWork.RetainTarget(ctx, models.EnrichmentTargetInput{PostUUID: post.UUID, URLUUID: url.URLUUID,
			CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, Policy: models.EnrichmentGalleryMetadataV1, Origin: "review"},
			models.EnrichmentSchedule{State: "pending", Priority: 20}, f.now)
		return err
	}))
	var err error
	_, f.token, err = f.service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID}}, nil)
	require.NoError(t, err)
	body, err := os.ReadFile("../../pkg/archive/testdata/enrichment-transcript-v1.json")
	require.NoError(t, err)
	var fixture struct {
		Initial  json.RawMessage `json:"initial"`
		Complete json.RawMessage `json:"complete"`
	}
	require.NoError(t, json.Unmarshal(body, &fixture))
	f.initial, f.complete = fixture.Initial, fixture.Complete
	return f
}

func (f *enrichmentHTTPFixture) request(t *testing.T, method, path string, input any, status int) []byte {
	t.Helper()
	body, err := archive.EncodeSourceJSON(input)
	require.NoError(t, err)
	r := httptest.NewRequest(method, ingestPath+path, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+f.token)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	require.Equal(t, status, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	return w.Body.Bytes()
}

func enrichmentHTTPValue[T any](t *testing.T, body []byte) T {
	t.Helper()
	var value T
	require.NoError(t, json.Unmarshal(body, &value))
	return value
}

func TestEnrichmentWorkerHTTPCheckpointFailureAndPublicationRecovery(t *testing.T) {
	f := newEnrichmentHTTPFixture(t)
	readyPath := "/enrichment/collections/" + f.collection.UUID + "/ready"
	ready := enrichmentHTTPValue[[]models.EnrichmentTarget](t, f.request(t, "POST", readyPath, map[string]any{"limit": 10}, 200))
	require.Equal(t, []models.EnrichmentTarget{*f.target}, ready)
	admission := map[string]any{"expected_revision": f.target.Revision, "policy_sha256": strings.Repeat("a", 64), "extractor_version": "1.32.15-dev"}
	admitPath := "/enrichment/targets/" + f.target.UUID + "/jobs"
	job := enrichmentHTTPValue[models.ArchiveJob](t, f.request(t, "POST", admitPath, admission, 200))
	require.Equal(t, job, enrichmentHTTPValue[models.ArchiveJob](t, f.request(t, "POST", admitPath, admission, 200)))
	path := "/enrichment/jobs/" + job.UUID
	described := enrichmentHTTPValue[ingest.EnrichmentExecution](t, f.request(t, "GET", path, nil, 200))
	require.Equal(t, job, *described.Job)
	require.Equal(t, *f.target, *described.Target)
	claim := map[string]any{"expected_revision": job.Revision, "owner_uuid": uuid.NewString(), "policy_sha256": strings.Repeat("a", 64), "extractor_version": "1.32.15-dev", "lease_seconds": 60}
	running := enrichmentHTTPValue[models.ArchiveJob](t, f.request(t, "POST", path+"/claim", claim, 200))
	require.Equal(t, running, enrichmentHTTPValue[models.ArchiveJob](t, f.request(t, "POST", path+"/claim", claim, 200)))
	claim["expected_revision"], claim["owner_uuid"] = running.Revision, uuid.NewString()
	f.request(t, "POST", path+"/claim", claim, 204)
	renew := map[string]any{"owner_uuid": running.OwnerUUID, "fence": running.Fence, "lease_seconds": 120}
	renewed := enrichmentHTTPValue[models.ArchiveJob](t, f.request(t, "POST", path+"/renew", renew, 200))
	require.True(t, renewed.LeaseUntil.After(*running.LeaseUntil))
	source := map[string]any{"owner_uuid": running.OwnerUUID, "fence": running.Fence, "url": "https://redgifs.com/watch/linked"}
	reservation := f.request(t, "POST", path+"/source", source, 200)
	require.JSONEq(t, string(reservation), string(f.request(t, "POST", path+"/source", source, 200)))
	require.Equal(t, true, enrichmentHTTPValue[map[string]any](t, reservation)["ready"])
	source["fence"] = running.Fence + 1
	f.request(t, "POST", path+"/source", source, 409)
	source["fence"], source["url"] = running.Fence, "https://unrelated.invalid/private"
	f.request(t, "POST", path+"/source", source, 400)
	checkpoint := map[string]any{"owner_uuid": running.OwnerUUID, "fence": running.Fence, "expected_revision": 0, "body": f.initial}
	head := enrichmentHTTPValue[models.EnrichmentCheckpointReceipt](t, f.request(t, "POST", path+"/checkpoint", checkpoint, 200))
	require.Equal(t, head, enrichmentHTTPValue[models.EnrichmentCheckpointReceipt](t, f.request(t, "POST", path+"/checkpoint", checkpoint, 200)))
	loaded := enrichmentHTTPValue[models.EnrichmentCheckpoint](t, f.request(t, "GET", path+"/checkpoint", nil, 200))
	require.Equal(t, head, loaded.EnrichmentCheckpointReceipt)
	require.JSONEq(t, string(f.initial), string(loaded.Body))
	publish := map[string]any{"owner_uuid": running.OwnerUUID, "fence": running.Fence, "checkpoint_revision": head.Revision, "checkpoint_sha256": head.Digest}
	f.request(t, "POST", path+"/publish", publish, 409) // pending child lookups cannot certify completion
	require.JSONEq(t, "null", string(f.request(t, "GET", path+"/publication", nil, 200)))
	failure := map[string]any{"owner_uuid": running.OwnerUUID, "fence": running.Fence, "error_code": "timeout"}
	failed := enrichmentHTTPValue[ingest.EnrichmentFailureReceipt](t, f.request(t, "POST", path+"/failure", failure, 200))
	require.Equal(t, "retry", failed.Outcome)
	require.Equal(t, failed, enrichmentHTTPValue[ingest.EnrichmentFailureReceipt](t, f.request(t, "POST", path+"/failure", failure, 200)))
	queued := enrichmentHTTPValue[ingest.EnrichmentExecution](t, f.request(t, "GET", path, nil, 200)).Job
	claim["expected_revision"] = queued.Revision
	f.request(t, "POST", path+"/claim", claim, 204)
	f.now = queued.AvailableAt
	next := enrichmentHTTPValue[models.ArchiveJob](t, f.request(t, "POST", path+"/claim", claim, 200))
	f.request(t, "POST", path+"/renew", renew, 409)
	checkpoint["owner_uuid"], checkpoint["fence"], checkpoint["expected_revision"] = next.OwnerUUID, next.Fence, head.Revision
	// A successor resuming unchanged evidence gets its original acknowledgement.
	require.Equal(t, head, enrichmentHTTPValue[models.EnrichmentCheckpointReceipt](t, f.request(t, "POST", path+"/checkpoint", checkpoint, 200)))
	checkpoint["body"] = f.complete
	complete := enrichmentHTTPValue[models.EnrichmentCheckpointReceipt](t, f.request(t, "POST", path+"/checkpoint", checkpoint, 200))
	publish["owner_uuid"], publish["fence"], publish["checkpoint_revision"], publish["checkpoint_sha256"] = next.OwnerUUID, next.Fence, complete.Revision, complete.Digest
	publication := enrichmentHTTPValue[models.EnrichmentPublication](t, f.request(t, "POST", path+"/publish", publish, 200))
	require.Equal(t, publication, enrichmentHTTPValue[models.EnrichmentPublication](t, f.request(t, "POST", path+"/publish", publish, 200)))
	require.Equal(t, publication, enrichmentHTTPValue[models.EnrichmentPublication](t, f.request(t, "GET", path+"/publication", nil, 200)))
	require.Equal(t, complete, enrichmentHTTPValue[models.EnrichmentCheckpointReceipt](t, f.request(t, "POST", path+"/checkpoint", checkpoint, 200)))
	require.JSONEq(t, "null", string(f.request(t, "GET", path+"/checkpoint", nil, 200)))
	release := enrichmentHTTPValue[models.EnrichmentCheckpointRelease](t, f.request(t, "GET", path+"/release", nil, 200))
	require.Equal(t, job.UUID, release.JobUUID)
	require.Len(t, release.Unresolved, 1)
	require.Equal(t, failed, enrichmentHTTPValue[ingest.EnrichmentFailureReceipt](t, f.request(t, "POST", path+"/failure", failure, 200)))
	require.JSONEq(t, "[]", string(f.request(t, "POST", readyPath, map[string]any{"limit": 10}, 200)))
}

type enrichmentUnreadBody struct{ read bool }

func (b *enrichmentUnreadBody) Read([]byte) (int, error) { b.read = true; return 0, io.EOF }

func TestEnrichmentWorkerHTTPRejectsUnauthorizedBodiesAndInvalidRequests(t *testing.T) {
	f := newEnrichmentHTTPFixture(t)
	path := "/enrichment/jobs/" + uuid.NewString()
	for _, suffix := range []string{"", "/claim", "/renew", "/checkpoint", "/publish", "/failure", "/publication", "/release"} {
		for _, method := range []string{"GET", "POST"} {
			body := &enrichmentUnreadBody{}
			r := httptest.NewRequest(method, ingestPath+path+suffix, body)
			r.AddCookie(&http.Cookie{Name: "session", Value: f.token})
			r.Header.Set("ApiKey", f.token)
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			require.Equal(t, 401, w.Code)
			require.False(t, body.read)
		}
	}
	f.request(t, "GET", path, nil, 404)
	f.request(t, "GET", "/enrichment/jobs/invalid", nil, 400)
	f.request(t, "POST", "/enrichment/targets/invalid/jobs", map[string]any{"expected_revision": 1}, 400)
	f.request(t, "POST", "/enrichment/collections/invalid/ready", map[string]any{"limit": 1}, 400)
	f.request(t, "POST", "/enrichment/collections/"+f.collection.UUID+"/ready", map[string]any{"limit": 101}, 400)
	for _, body := range []any{map[string]any{"producer_uuid": f.producer.UUID}, map[string]any{"error_code": "succeeded"}, map[string]any{"error_code": "private output"}} {
		f.request(t, "POST", path+"/failure", body, 400)
	}
	for _, seconds := range []int{0, 4, 901} {
		f.request(t, "POST", path+"/claim", map[string]any{"lease_seconds": seconds}, 400)
		f.request(t, "POST", path+"/renew", map[string]any{"lease_seconds": seconds}, 400)
	}
	r := httptest.NewRequest("POST", ingestPath+path+"/checkpoint", strings.NewReader(strings.Repeat(" ", maxEnrichmentRequestBytes+1)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+f.token)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	require.Equal(t, 400, w.Code)
	capabilities := enrichmentHTTPValue[map[string]any](t, f.request(t, "GET", "/capabilities", nil, 200))
	require.EqualValues(t, 1, capabilities["enrichment_protocol"])
	require.EqualValues(t, archive.MaxEnrichmentTranscriptBytes, capabilities["max_enrichment_checkpoint_bytes"])
}

func TestEnrichmentWorkerHTTPCollectionAndProducerIsolation(t *testing.T) {
	f := newEnrichmentHTTPFixture(t)
	job, err := f.worker.Admit(t.Context(), f.token, f.target.UUID, f.target.Revision, strings.Repeat("a", 64), "1.32.15-dev")
	require.NoError(t, err)
	running, err := f.worker.Claim(t.Context(), f.token, job.UUID, job.Revision, uuid.NewString(), strings.Repeat("a", 64), "1.32.15-dev", time.Minute)
	require.NoError(t, err)
	_, err = f.worker.Checkpoint(t.Context(), f.token, running.Lease(), 0, f.initial)
	require.NoError(t, err)
	var producer *models.IngestProducer
	var other *models.SourceCollection
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		producer, err = f.repo.Ingest.CreateProducer(ctx, "Other worker")
		if err != nil {
			return err
		}
		other, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Other source", Kind: "feed", State: "active"}})
		return err
	}))
	_, f.token, err = f.service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: other.UUID}}, nil)
	require.NoError(t, err)
	path := "/enrichment/jobs/" + job.UUID
	for _, suffix := range []string{"", "/checkpoint", "/publication", "/release"} {
		f.request(t, "GET", path+suffix, nil, 403)
	}
	f.request(t, "POST", "/enrichment/collections/"+f.collection.UUID+"/ready", map[string]any{"limit": 10}, 403)
	f.request(t, "POST", "/enrichment/targets/"+f.target.UUID+"/jobs", map[string]any{"expected_revision": f.target.Revision, "policy_sha256": strings.Repeat("a", 64), "extractor_version": "1.32.15-dev"}, 403)
	_, f.token, err = f.service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID}}, nil)
	require.NoError(t, err)
	f.request(t, "GET", path+"/checkpoint", nil, 200) // shared collection evidence is resumable
	f.request(t, "POST", path+"/failure", map[string]any{"owner_uuid": running.OwnerUUID, "fence": running.Fence, "error_code": "timeout"}, 409)
	f.request(t, "POST", path+"/checkpoint", map[string]any{"owner_uuid": running.OwnerUUID, "fence": running.Fence, "expected_revision": 0, "body": f.initial}, 409)
}
