package sqlite_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceRunReadyOnlyExposesPermittedRootPolicyAndOperation(t *testing.T) {
	f := newSourceRunFixture(t)
	input := f.request()
	wanted := f.submit(t, input)
	otherPolicy := f.request()
	otherPolicy.PolicySHA256 = strings.Repeat("b", 64)
	f.submit(t, otherPolicy)
	enrich := f.request()
	enrich.Operation = "enrich"
	f.submit(t, enrich)
	definition := f.collection.SourceCollectionDefinition
	definition.TargetURL, definition.PathPrefix = "https://www.reddit.com/user/other/submitted/", "Other"
	other := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: definition})
	otherRequest := f.request()
	otherRequest.CollectionUUID = other.UUID
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceRun.Submit(ctx, f.producer.UUID, otherRequest, f.now, 100)
		return err
	}))
	ready, err := f.coordinator.Ready(t.Context(), f.token, f.root.UUID, input.PolicySHA256, 0)
	require.NoError(t, err)
	require.Equal(t, []models.SourceRunCandidate{{Sequence: wanted.Sequence, UUID: wanted.UUID}}, ready)
	require.Equal(t, wanted, f.find(t, wanted.UUID), "discovery must not claim or modify work")
	_, err = f.coordinator.Ready(t.Context(), f.token, uuid.NewString(), input.PolicySHA256, 0)
	require.ErrorIs(t, err, ingest.ErrForbidden)
	_, err = f.coordinator.Ready(t.Context(), f.token, f.root.UUID, "invalid", 0)
	require.ErrorIs(t, err, models.ErrSourceRunInvalid)
	_, err = f.coordinator.Ready(t.Context(), f.token, f.root.UUID, input.PolicySHA256, -1)
	require.ErrorIs(t, err, models.ErrSourceRunInvalid)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.Ingest.RevokeCredential(ctx, f.credential.UUID)
	}))
	_, err = f.coordinator.Ready(t.Context(), f.token, f.root.UUID, input.PolicySHA256, 0)
	require.ErrorIs(t, err, ingest.ErrUnauthorized)
}

func TestSourceRunReadyRetainsExpiryBackoffAndExplicitDeferral(t *testing.T) {
	f := newSourceRunFixture(t)
	running := f.claim(t, f.submit(t, f.request()))
	ready := func() []models.SourceRunCandidate {
		rows, err := f.coordinator.Ready(t.Context(), f.token, f.root.UUID, running.PolicySHA256, 0)
		require.NoError(t, err)
		return rows
	}
	require.Empty(t, ready())
	f.now = running.LeaseUntil.Add(time.Millisecond)
	require.Equal(t, []models.SourceRunCandidate{{Sequence: running.Sequence, UUID: running.UUID}}, ready())
	require.Equal(t, "running", f.find(t, running.UUID).State, "read-only discovery cannot recover a lease")
	require.Nil(t, f.claim(t, running), "recovery applies backoff before another worker can take over")
	recovered := f.find(t, running.UUID)
	require.Equal(t, "queued", recovered.State)
	require.Equal(t, "lease_expired", recovered.ErrorCode)
	require.Empty(t, ready())
	f.now = recovered.AvailableAt.Add(time.Millisecond)
	require.Len(t, ready(), 1)
	claimed := f.claim(t, recovered)
	require.Greater(t, claimed.Fence, running.Fence)
	f.finish(t, claimed, "deferred")
	f.now = f.now.Add(24 * time.Hour)
	require.Empty(t, ready(), "a timer must not restart explicitly deferred source work")
}

func TestSourceRunReadyPagesPastBusyWorkWithoutScanningCompletedHistory(t *testing.T) {
	f := newSourceRunFixture(t)
	var expected []models.SourceRunCandidate
	for i := 0; i < 55; i++ {
		input := f.request()
		input.CooldownSeconds = i
		r := f.submit(t, input)
		expected = append(expected, models.SourceRunCandidate{Sequence: r.Sequence, UUID: r.UUID})
	}
	first, err := f.coordinator.Ready(t.Context(), f.token, f.root.UUID, f.request().PolicySHA256, 0)
	require.NoError(t, err)
	require.Equal(t, expected[:50], first)
	second, err := f.coordinator.Ready(t.Context(), f.token, f.root.UUID, f.request().PolicySHA256, first[49].Sequence)
	require.NoError(t, err)
	require.Equal(t, expected[50:], second)
	end, err := f.coordinator.Ready(t.Context(), f.token, f.root.UUID, f.request().PolicySHA256, second[4].Sequence)
	require.NoError(t, err)
	require.Empty(t, end)
	claimed := f.claim(t, f.find(t, expected[0].UUID))
	f.finish(t, claimed, "succeeded")
	first, err = f.coordinator.Ready(t.Context(), f.token, f.root.UUID, f.request().PolicySHA256, 0)
	require.NoError(t, err)
	require.Equal(t, expected[1:51], first, "completed history stays outside worker discovery")
}
