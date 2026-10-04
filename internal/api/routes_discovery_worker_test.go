package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

type discoveryHTTPFixture struct {
	*enrichmentHTTPFixture
	listing *models.DiscoveryListing
	page    json.RawMessage
}

func newDiscoveryHTTPFixture(t *testing.T) *discoveryHTTPFixture {
	t.Helper()
	return newDiscoveryHTTPFixtureAt(t, time.Date(2026, 10, 4, 14, 0, 0, 0, time.UTC))
}

func newDiscoveryHTTPFixtureAt(t *testing.T, now time.Time) *discoveryHTTPFixture {
	return newDiscoveryHTTPFixtureForPolicy(t, now, strings.Repeat("a", 64))
}

func newDiscoveryHTTPFixtureForPolicy(t *testing.T, now time.Time, policy string) *discoveryHTTPFixture {
	t.Helper()
	f := &discoveryHTTPFixture{enrichmentHTTPFixture: newEnrichmentHTTPFixture(t)}
	f.now = now
	worker := ingest.NewDiscoveryCoordinator(f.service)
	worker.Now = func() time.Time { return f.now }
	f.handler = (&ingestRoutes{service: f.service, enrichment: f.worker, discovery: worker}).router()
	body, err := os.ReadFile("../../pkg/archive/testdata/discovery-pages-v1.json")
	require.NoError(t, err)
	var corpus struct {
		Pages []struct {
			Page json.RawMessage `json:"page"`
		} `json:"pages"`
	}
	require.NoError(t, json.Unmarshal(body, &corpus))
	f.page = corpus.Pages[1].Page
	page, err := archive.ParseDiscoveryPage(f.page)
	require.NoError(t, err)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		account, err := f.repo.SourceAccount.Create(ctx, "native:reddit", "Source account")
		if err != nil {
			return err
		}
		f.listing, err = f.repo.DiscoveryJob.CreateListing(ctx, models.DiscoveryListingInput{UUID: uuid.NewString(), AccountUUID: account.UUID,
			CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, ProfileURL: page.URL,
			PolicySHA256: policy, ExtractorVersion: page.ExtractorVersion, NotBefore: f.now}, f.now)
		return err
	}))
	return f
}

func (f *discoveryHTTPFixture) admit(t *testing.T) models.ArchiveJob {
	t.Helper()
	return enrichmentHTTPValue[models.ArchiveJob](t, f.request(t, "POST", "/discovery/listings/"+f.listing.UUID+"/jobs", map[string]any{
		"expected_definition_sha256": f.listing.Digest, "policy_sha256": f.listing.PolicySHA256, "extractor_version": f.listing.ExtractorVersion,
	}, 200))
}

func (f *discoveryHTTPFixture) claim(t *testing.T, job models.ArchiveJob, owner string) models.ArchiveJob {
	t.Helper()
	return enrichmentHTTPValue[models.ArchiveJob](t, f.request(t, "POST", "/discovery/jobs/"+job.UUID+"/claim", map[string]any{
		"expected_revision": job.Revision, "owner_uuid": owner, "policy_sha256": f.listing.PolicySHA256,
		"extractor_version": f.listing.ExtractorVersion, "lease_seconds": 60,
	}, 200))
}

func TestDiscoveryWorkerHTTPPageAndFailureAcknowledgements(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	job := f.admit(t)
	require.Equal(t, job, f.admit(t))
	path := "/discovery/jobs/" + job.UUID
	described := enrichmentHTTPValue[ingest.DiscoveryJobDescription](t, f.request(t, "GET", path, nil, 200))
	require.Equal(t, f.listing, described.Listing)
	require.Nil(t, described.Cursor)
	require.Nil(t, described.Receipt)
	owner := uuid.NewString()
	running := f.claim(t, job, owner)
	require.Equal(t, running, f.claim(t, job, owner), "lost claim response reuses its attempt")
	reservation := map[string]any{"owner_uuid": owner, "fence": running.Fence, "url": f.listing.ProfileURL}
	ready := enrichmentHTTPValue[map[string]any](t, f.request(t, "POST", path+"/source", reservation, 200))
	require.Equal(t, true, ready["ready"])
	reservation["url"] = "https://www.reddit.com/user/unrelated/submitted/"
	f.request(t, "POST", path+"/source", reservation, 400)
	f.now = f.now.Add(15 * time.Second)
	renewed := enrichmentHTTPValue[models.ArchiveJob](t, f.request(t, "POST", path+"/renew", map[string]any{
		"owner_uuid": owner, "fence": running.Fence, "lease_seconds": 60,
	}, 200))
	require.True(t, renewed.LeaseUntil.After(*running.LeaseUntil))
	input := map[string]any{"owner_uuid": owner, "fence": running.Fence, "ordinal": 1, "body": f.page}
	first := enrichmentHTTPValue[models.DiscoveryPageReceipt](t, f.request(t, "POST", path+"/page", input, 200))
	require.False(t, first.Complete)
	require.Equal(t, 3, first.RecordCount)
	f.now = f.now.Add(2 * time.Minute)
	require.Equal(t, first, enrichmentHTTPValue[models.DiscoveryPageReceipt](t, f.request(t, "POST", path+"/page", input, 200)))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		stored, err := f.repo.DiscoveryJob.Page(ctx, f.listing.UUID, 1)
		require.NoError(t, err)
		original, err := archive.ParseDiscoveryPage(f.page)
		require.NoError(t, err)
		require.Equal(t, original.Body(), stored.Body, "HTTP retains exact source numbers and compact shared metadata")
		return nil
	}))
	next := f.admit(t)
	require.NotEqual(t, job.UUID, next.UUID)
	path = "/discovery/jobs/" + next.UUID
	described = enrichmentHTTPValue[ingest.DiscoveryJobDescription](t, f.request(t, "GET", path, nil, 200))
	require.Equal(t, map[string]string{"after": "t3_abc123"}, described.Cursor)
	owner = uuid.NewString()
	running = f.claim(t, next, owner)
	failureInput := map[string]any{"owner_uuid": owner, "fence": running.Fence, "error_code": "rate_limited"}
	failure := f.request(t, "POST", path+"/failure", failureInput, 200)
	require.Equal(t, failure, f.request(t, "POST", path+"/failure", failureInput, 200))
	described = enrichmentHTTPValue[ingest.DiscoveryJobDescription](t, f.request(t, "GET", path, nil, 200))
	require.Equal(t, "queued", described.Job.State)
	require.Equal(t, f.now.Add(time.Hour), described.Job.AvailableAt)
	f.now = f.now.Add(time.Hour)
	running = f.claim(t, *described.Job, uuid.NewString())
	final, err := archive.DecodeJSONObject(f.page, archive.MaxDiscoveryPageBytes)
	require.NoError(t, err)
	final["cursor"], final["next_cursor"], final["complete"], final["records"] = described.Cursor, nil, true, []any{}
	finalBody, err := archive.EncodeSourceJSON(final)
	require.NoError(t, err)
	last := enrichmentHTTPValue[models.DiscoveryPageReceipt](t, f.request(t, "POST", path+"/page", map[string]any{
		"owner_uuid": running.OwnerUUID, "fence": running.Fence, "ordinal": 2, "body": json.RawMessage(finalBody),
	}, 200))
	require.True(t, last.Complete)
	require.Equal(t, failure, f.request(t, "POST", path+"/failure", failureInput, 200), "old failure replay cannot affect later success")
	described = enrichmentHTTPValue[ingest.DiscoveryJobDescription](t, f.request(t, "GET", path, nil, 200))
	require.Equal(t, "succeeded", described.Job.State)
	require.Equal(t, &last, described.Receipt)
	require.Equal(t, *described.Job, f.admit(t), "a final page cannot admit more enumeration")
}

func TestDiscoveryWorkerHTTPRejectsInvalidAndUnauthorizedRequests(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	path := "/discovery/jobs/" + uuid.NewString()
	for _, suffix := range []string{"", "/claim", "/renew", "/source", "/page", "/failure"} {
		body := &enrichmentUnreadBody{}
		r := httptest.NewRequest("POST", ingestPath+path+suffix, body)
		r.AddCookie(&http.Cookie{Name: "session", Value: f.token})
		r.Header.Set("ApiKey", f.token)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		require.Equal(t, 401, w.Code)
		require.False(t, body.read)
	}
	f.request(t, "GET", path, nil, 404)
	f.request(t, "GET", "/discovery/jobs/invalid", nil, 400)
	f.request(t, "POST", "/discovery/listings/invalid/jobs", map[string]any{}, 400)
	f.request(t, "POST", "/discovery/listings/"+f.listing.UUID+"/jobs", map[string]any{
		"expected_definition_sha256": strings.Repeat("f", 64), "policy_sha256": f.listing.PolicySHA256, "extractor_version": f.listing.ExtractorVersion,
	}, 409)
	for _, seconds := range []int{0, 4, 901} {
		f.request(t, "POST", path+"/claim", map[string]any{"lease_seconds": seconds}, 400)
		f.request(t, "POST", path+"/renew", map[string]any{"lease_seconds": seconds}, 400)
	}
	for _, input := range []any{map[string]any{"producer_uuid": f.producer.UUID}, map[string]any{"job_uuid": uuid.NewString()}, map[string]any{"error_code": "succeeded"}} {
		f.request(t, "POST", path+"/failure", input, 400)
	}
	for _, body := range []string{strings.Repeat(" ", maxDiscoveryRequestBytes+1), `{"ordinal":1,"ordinal":2}`} {
		r := httptest.NewRequest("POST", ingestPath+path+"/page", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+f.token)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		require.Equal(t, 400, w.Code)
	}
	capabilities := enrichmentHTTPValue[map[string]any](t, f.request(t, "GET", "/capabilities", nil, 200))
	require.EqualValues(t, 1, capabilities["discovery_protocol"])
	require.EqualValues(t, 1, capabilities["discovery_source_pacing_protocol"])
	require.EqualValues(t, archive.MaxDiscoveryPageBytes, capabilities["max_discovery_page_bytes"])
	require.EqualValues(t, 1, capabilities["discovery_dispatch_protocol"])
}

func TestDiscoveryWorkerHTTPCollectionAndProducerIsolation(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	job := f.admit(t)
	running := f.claim(t, job, uuid.NewString())
	path := "/discovery/jobs/" + job.UUID
	input := map[string]any{"owner_uuid": running.OwnerUUID, "fence": running.Fence, "ordinal": 1, "body": f.page}
	var other *models.SourceCollection
	var producer *models.IngestProducer
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		other, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Other collection", Kind: "feed", State: "active"}})
		if err != nil {
			return err
		}
		producer, err = f.repo.Ingest.CreateProducer(ctx, "Other worker")
		return err
	}))
	_, token, err := f.service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: other.UUID}}, nil)
	require.NoError(t, err)
	original := f.token
	f.token = token
	f.request(t, "GET", path, nil, 403)
	f.request(t, "POST", path+"/page", input, 403)
	credential, token, err := f.service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID}}, nil)
	require.NoError(t, err)
	f.token = token
	f.request(t, "GET", path, nil, 200)
	f.request(t, "POST", path+"/page", input, 409)
	f.request(t, "POST", path+"/failure", map[string]any{"owner_uuid": running.OwnerUUID, "fence": running.Fence, "error_code": "timeout"}, 409)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.Ingest.RevokeCredential(ctx, credential.UUID) }))
	f.request(t, "GET", path, nil, 401)
	f.token = original
	f.request(t, "POST", path+"/page", input, 200)
	bad, err := archive.EncodeSourceJSON(input)
	require.NoError(t, err)
	bad = bytes.Replace(bad, []byte(`"next_cursor":{"after":"t3_abc123"}`), []byte(`"next_cursor":{"after":"t3_changed"}`), 1)
	r := httptest.NewRequest("POST", ingestPath+path+"/page", bytes.NewReader(bad))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+f.token)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	require.Equal(t, 409, w.Code, w.Body.String())
}
