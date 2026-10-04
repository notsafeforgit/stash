package archive

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryDetailJobPinsCandidateOriginalEvidenceAndRuntime(t *testing.T) {
	input := models.DiscoveryDetailJobArguments{Version: 1, Generation: 1, TargetUUID: uuid.NewString(), TargetRevision: 2, SourceSHA256: strings.Repeat("a", 64), PostUUID: uuid.NewString(), PostRevision: 1,
		CandidateSequence: 3, Namespace: "native:reddit", Value: "abc123", URL: "https://www.reddit.com/comments/abc123", ListingUUID: uuid.NewString(), DefinitionSHA256: strings.Repeat("b", 64), PageOrdinal: 1, PageSHA256: strings.Repeat("c", 64),
		CollectionUUID: uuid.NewString(), CollectionRevision: 2, PolicySHA256: strings.Repeat("d", 64), ExtractorVersion: "fixture-runtime", CapturePolicy: CaptureContextPolicy}
	original, err := PrepareDiscoveryDetailJob(input)
	require.NoError(t, err)
	job := &models.ArchiveJob{Kind: original.Kind, WorkKey: original.WorkKey, ResourceKey: original.ResourceKey, Arguments: original.Arguments, MaxAttempts: original.MaxAttempts}
	decoded, err := DecodeDiscoveryDetailJob(job)
	require.NoError(t, err)
	require.Equal(t, input, *decoded)
	for _, change := range []func(*models.DiscoveryDetailJobArguments){
		func(v *models.DiscoveryDetailJobArguments) { v.TargetRevision++ },
		func(v *models.DiscoveryDetailJobArguments) { v.SourceSHA256 = strings.Repeat("b", 64) },
		func(v *models.DiscoveryDetailJobArguments) { v.PageSHA256 = strings.Repeat("d", 64) },
		func(v *models.DiscoveryDetailJobArguments) { v.PageOrdinal++ },
		func(v *models.DiscoveryDetailJobArguments) { v.CandidateSequence++ },
		func(v *models.DiscoveryDetailJobArguments) { v.PostRevision++ },
		func(v *models.DiscoveryDetailJobArguments) { v.PolicySHA256 = strings.Repeat("e", 64) },
		func(v *models.DiscoveryDetailJobArguments) { v.ExtractorVersion = "other-runtime" },
		func(v *models.DiscoveryDetailJobArguments) { v.Generation++ },
	} {
		changed := input
		change(&changed)
		next, err := PrepareDiscoveryDetailJob(changed)
		require.NoError(t, err)
		require.NotEqual(t, original.RequestUUID, next.RequestUUID)
		require.NotEqual(t, original.WorkKey, next.WorkKey)
		require.Equal(t, original.ResourceKey, next.ResourceKey, "the collection shares a metadata reservation")
	}
	for _, change := range []func(*models.DiscoveryDetailJobArguments){
		func(v *models.DiscoveryDetailJobArguments) { v.Generation = 0 },
		func(v *models.DiscoveryDetailJobArguments) { v.CandidateSequence = 0 },
		func(v *models.DiscoveryDetailJobArguments) { v.SourceSHA256 = "filename" },
		func(v *models.DiscoveryDetailJobArguments) { v.URL = "https://www.reddit.com/user/abc123" },
		func(v *models.DiscoveryDetailJobArguments) { v.Value = "different" },
		func(v *models.DiscoveryDetailJobArguments) { v.ExtractorVersion = "runtime\nprivate message" },
		func(v *models.DiscoveryDetailJobArguments) { v.CapturePolicy = "" },
	} {
		changed := input
		change(&changed)
		_, err := PrepareDiscoveryDetailJob(changed)
		require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
	}
	changed := *job
	changed.Arguments = append(append([]byte{}, job.Arguments[:len(job.Arguments)-1]...), []byte(`,"settings":{}}`)...)
	_, err = DecodeDiscoveryDetailJob(&changed)
	require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
	changed = *job
	changed.ResourceKey = strings.Repeat("f", 64)
	_, err = DecodeDiscoveryDetailJob(&changed)
	require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
}
