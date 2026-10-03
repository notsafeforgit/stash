package archive

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestEnrichmentJobPinsTargetSourceAndRuntime(t *testing.T) {
	input := models.EnrichmentJobArguments{Version: 1, TargetUUID: uuid.NewString(), TargetRevision: 3,
		PostUUID: uuid.NewString(), CollectionUUID: uuid.NewString(), CollectionRevision: 5,
		PolicySHA256: strings.Repeat("a", 64), ExtractorVersion: "1.32.15-dev"}
	submission, err := PrepareEnrichmentJob(input)
	require.NoError(t, err)
	replay, err := PrepareEnrichmentJob(input)
	require.NoError(t, err)
	require.Equal(t, submission, replay)
	job := &models.ArchiveJob{Kind: submission.Kind, WorkKey: submission.WorkKey, ResourceKey: submission.ResourceKey,
		Arguments: submission.Arguments, MaxAttempts: submission.MaxAttempts}
	decoded, err := DecodeEnrichmentJob(job)
	require.NoError(t, err)
	require.Equal(t, input, *decoded)
	for _, change := range []func(*models.EnrichmentJobArguments){
		func(v *models.EnrichmentJobArguments) { v.TargetRevision++ },
		func(v *models.EnrichmentJobArguments) { v.CollectionRevision++ },
		func(v *models.EnrichmentJobArguments) { v.PolicySHA256 = strings.Repeat("b", 64) },
		func(v *models.EnrichmentJobArguments) { v.ExtractorVersion = "next-runtime" },
		func(v *models.EnrichmentJobArguments) { id := uuid.NewString(); v.RootUUID = &id },
	} {
		changed := input
		change(&changed)
		next, err := PrepareEnrichmentJob(changed)
		require.NoError(t, err)
		require.NotEqual(t, submission.RequestUUID, next.RequestUUID)
		require.NotEqual(t, submission.WorkKey, next.WorkKey)
		require.Equal(t, submission.ResourceKey, next.ResourceKey, "different work in the same collection still serializes")
	}
	for _, change := range []func(*models.EnrichmentJobArguments){
		func(v *models.EnrichmentJobArguments) { v.Version = 2 },
		func(v *models.EnrichmentJobArguments) { v.TargetRevision = 0 },
		func(v *models.EnrichmentJobArguments) { v.PostUUID = "name" },
		func(v *models.EnrichmentJobArguments) { v.CollectionUUID = uuid.Nil.String() },
		func(v *models.EnrichmentJobArguments) { v.PolicySHA256 = "raw settings" },
		func(v *models.EnrichmentJobArguments) { v.ExtractorVersion = "version\nprivate log" },
	} {
		changed := input
		change(&changed)
		_, err := PrepareEnrichmentJob(changed)
		require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
	}
	job.ResourceKey = strings.Repeat("0", 64)
	_, err = DecodeEnrichmentJob(job)
	require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
}
