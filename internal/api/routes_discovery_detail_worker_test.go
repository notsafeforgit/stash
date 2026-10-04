package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

// Uses the real imported discovery fixture, application activation and retained
// listing from the HTTP recovery test, then drives the public worker contract.
func exerciseDiscoveryDetailHTTP(t *testing.T, service *ingest.Service, handler http.Handler, token string, review models.DiscoveryMatchReview, listing models.DiscoveryListingInput, body json.RawMessage) {
	t.Helper()
	request := func(method, path string, input any, token string, status int) []byte {
		var encoded []byte
		if input != nil {
			var err error
			encoded, err = json.Marshal(input)
			require.NoError(t, err)
		}
		r := httptest.NewRequest(method, ingestPath+"/discovery-details"+path, bytes.NewReader(encoded))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		return w.Body.Bytes()
	}
	input := map[string]any{"expected_target_revision": review.Target.Revision, "candidate_sequence": review.Candidate.Sequence, "policy_sha256": listing.PolicySHA256, "extractor_version": listing.ExtractorVersion}
	path := "/targets/" + review.Target.UUID + "/jobs"
	job := enrichmentHTTPValue[models.ArchiveJob](t, request("POST", path, input, token, http.StatusOK))
	require.Equal(t, job, enrichmentHTTPValue[models.ArchiveJob](t, request("POST", path, input, token, http.StatusOK)))
	require.Equal(t, job, enrichmentHTTPValue[models.ArchiveJob](t, request("GET", "/jobs/"+job.UUID, nil, token, http.StatusOK)))
	work, err := archive.DecodeDiscoveryDetailJob(&job)
	require.NoError(t, err)
	require.Equal(t, review.Candidate.URL, work.URL)
	ready := enrichmentHTTPValue[[]models.DiscoveryJobCandidate](t, request("POST", "/collections/"+listing.CollectionUUID+"/jobs/ready", map[string]any{"policy_sha256": listing.PolicySHA256, "extractor_version": listing.ExtractorVersion, "after": 0, "limit": 10}, token, 200))
	require.Contains(t, ready, models.DiscoveryJobCandidate{Sequence: job.Sequence, UUID: job.UUID})
	collections := enrichmentHTTPValue[[]models.EnrichmentCollectionCandidate](t, request("POST", "/collections/ready", map[string]any{"policy_sha256": listing.PolicySHA256, "extractor_version": listing.ExtractorVersion, "limit": 10}, token, 200))
	require.Equal(t, []models.EnrichmentCollectionCandidate{{UUID: listing.CollectionUUID}}, collections)
	request("POST", "/collections/ready", map[string]any{"policy_sha256": listing.PolicySHA256, "extractor_version": listing.ExtractorVersion, "after": "invalid"}, token, 400)
	var outsider *models.IngestProducer
	require.NoError(t, service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		outsider, err = service.Repo.Ingest.CreateProducer(ctx, "Other detail worker")
		return err
	}))
	_, otherToken, err := service.IssueCredential(t.Context(), outsider.UUID, []models.IngestScope{{CollectionUUID: listing.CollectionUUID}}, nil)
	require.NoError(t, err)
	base := "/jobs/" + job.UUID
	claim := map[string]any{"expected_revision": job.Revision, "owner_uuid": uuid.NewString(), "policy_sha256": listing.PolicySHA256, "extractor_version": listing.ExtractorVersion, "lease_seconds": 60}
	running := enrichmentHTTPValue[models.ArchiveJob](t, request("POST", base+"/claim", claim, token, http.StatusOK))
	require.Equal(t, running, enrichmentHTTPValue[models.ArchiveJob](t, request("POST", base+"/claim", claim, token, http.StatusOK)))
	request("POST", base+"/source", map[string]any{"owner_uuid": running.OwnerUUID, "fence": running.Fence, "url": "https://unrelated.invalid/private"}, token, http.StatusBadRequest)
	request("POST", "/collections/"+listing.CollectionUUID+"/jobs/ready", map[string]any{"policy_sha256": listing.PolicySHA256, "extractor_version": listing.ExtractorVersion, "limit": -1}, token, http.StatusBadRequest)
	save := map[string]any{"owner_uuid": running.OwnerUUID, "fence": running.Fence, "expected_revision": 0, "body": body}
	request("POST", base+"/checkpoint", save, otherToken, http.StatusConflict)
	checkpoint := enrichmentHTTPValue[models.EnrichmentCheckpointReceipt](t, request("POST", base+"/checkpoint", save, token, http.StatusOK))
	require.Equal(t, checkpoint, enrichmentHTTPValue[models.EnrichmentCheckpointReceipt](t, request("POST", base+"/checkpoint", save, token, http.StatusOK)))
	head := enrichmentHTTPValue[models.EnrichmentCheckpoint](t, request("GET", base+"/checkpoint", nil, token, http.StatusOK))
	require.Equal(t, checkpoint, head.EnrichmentCheckpointReceipt)
	finish := map[string]any{"owner_uuid": running.OwnerUUID, "fence": running.Fence, "checkpoint_revision": checkpoint.Revision, "checkpoint_sha256": strings.Repeat("f", 64)}
	request("POST", base+"/complete", finish, token, http.StatusConflict)
	finish["checkpoint_sha256"] = checkpoint.Digest
	result := enrichmentHTTPValue[models.DiscoveryDetailResult](t, request("POST", base+"/complete", finish, token, http.StatusOK))
	require.Equal(t, "corroborated", result.Evidence.Status)
	require.Equal(t, result, enrichmentHTTPValue[models.DiscoveryDetailResult](t, request("POST", base+"/complete", finish, token, http.StatusOK)))
	require.Equal(t, result, enrichmentHTTPValue[models.DiscoveryDetailResult](t, request("GET", base+"/result", nil, token, http.StatusOK)))
	request("POST", base+"/retry", map[string]any{}, token, http.StatusConflict)
	request("POST", base+"/checkpoint", map[string]any{"settings": map[string]any{}}, token, http.StatusBadRequest)
	require.NoError(t, service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := service.Repo.DiscoveryMatch.Review(ctx, review.Target.UUID)
		require.NoError(t, err)
		require.Equal(t, review, *current)
		return nil
	}))
}

func TestDiscoveryDetailHTTPAuthenticatesBeforeReadingBodies(t *testing.T) {
	f := newEnrichmentHTTPFixture(t)
	for _, suffix := range []string{"checkpoint", "complete", "claim", "failure"} {
		body := &enrichmentUnreadBody{}
		r := httptest.NewRequest("POST", ingestPath+"/discovery-details/jobs/"+uuid.NewString()+"/"+suffix, body)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.False(t, body.read)
	}
}
