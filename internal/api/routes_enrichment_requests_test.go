package api

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestEnrichmentRequestAPIKeepsRecoveryPriorityAndFencesResults(t *testing.T) {
	f := newEnrichmentHTTPFixture(t)
	base := "/enrichment/targets/" + f.target.UUID
	require.Equal(t, "null", strings.TrimSpace(string(f.request(t, "POST", base+"/reuse", map[string]any{"expected_revision": f.target.Revision}, 200))))
	job := enrichmentHTTPValue[models.ArchiveJob](t, f.request(t, "POST", base+"/jobs", map[string]any{
		"expected_revision": f.target.Revision, "policy_sha256": strings.Repeat("a", 64), "extractor_version": "1.32.15-dev"}, 200))
	path := "/enrichment/jobs/" + job.UUID
	running := enrichmentHTTPValue[models.ArchiveJob](t, f.request(t, "POST", path+"/claim", map[string]any{
		"expected_revision": job.Revision, "owner_uuid": uuid.NewString(), "policy_sha256": strings.Repeat("a", 64), "extractor_version": "1.32.15-dev", "lease_seconds": 60}, 200))
	input := map[string]any{"owner_uuid": running.OwnerUUID, "fence": running.Fence, "request_uuid": uuid.NewString(), "url": "https://oauth.reddit.com/comments/abc123", "class": "routine", "operation": "other"}
	f.request(t, "POST", path+"/requests", input, 400)
	input["class"] = "recovery"
	grant := enrichmentHTTPValue[models.SourceRequestPermit](t, f.request(t, "POST", path+"/requests", input, 200))
	require.True(t, grant.Ready)
	result := map[string]any{"owner_uuid": running.OwnerUUID, "fence": running.Fence + 1, "request_uuid": grant.RequestUUID, "status": 429, "retry_after_seconds": 120}
	f.request(t, "POST", path+"/requests/result", result, 409)
	result["fence"] = running.Fence
	f.request(t, "POST", path+"/requests/result", result, 200)
	f.request(t, "POST", path+"/requests/result", result, 200)
	input["request_uuid"] = uuid.NewString()
	grant = enrichmentHTTPValue[models.SourceRequestPermit](t, f.request(t, "POST", path+"/requests", input, 200))
	require.False(t, grant.Ready)
	require.Positive(t, grant.WaitMS)
}
