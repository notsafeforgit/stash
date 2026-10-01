package sqlite_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type sourceRunFixture struct {
	db          *sqlite.Database
	repo        models.Repository
	service     *ingest.Service
	coordinator *ingest.RunCoordinator
	now         time.Time
	producer    *models.IngestProducer
	credential  *models.IngestCredential
	token       string
	root        *models.MediaRoot
	collection  *models.SourceCollection
}

func TestSourceRunLeaseExpiryDuringPublicationRollsBack(t *testing.T) {
	f := newSourceRunFixture(t)
	r := f.claim(t, f.submit(t, f.request()))
	clockReads := 0
	f.coordinator.Now = func() time.Time {
		clockReads++
		if clockReads >= 3 {
			return *r.LeaseUntil
		}
		return f.now
	}
	_, err := f.coordinator.Finish(t.Context(), f.token, r.Lease(), models.SourceRunOutcome{State: "succeeded"})
	require.ErrorIs(t, err, models.ErrSourceRunLease)
	require.Equal(t, "running", f.find(t, r.UUID).State)
	attempts, err := f.coordinator.Attempts(t.Context(), f.token, r.UUID, 0)
	require.NoError(t, err)
	require.Equal(t, "running", attempts[0].Outcome)
}

func TestSourceRunDisjointWindowCapacityDoesNotLoseRequests(t *testing.T) {
	f := newSourceRunFixture(t)
	input := f.request()
	base := f.now.AddDate(-1, 0, 0)
	input.Window = models.SourceWindow{Since: &base, Until: base.Add(time.Minute)}
	r := f.submit(t, input)
	for i := 1; i < 64; i++ {
		from := base.Add(time.Duration(i*2) * time.Minute)
		input.RequestUUID = uuid.NewString()
		input.Window = models.SourceWindow{Since: &from, Until: from.Add(time.Minute)}
		r = f.submit(t, input)
	}
	require.Len(t, r.Pending, 64)
	from := base.Add(128 * time.Minute)
	input.RequestUUID = uuid.NewString()
	input.Window = models.SourceWindow{Since: &from, Until: from.Add(time.Minute)}
	_, err := f.coordinator.Submit(t.Context(), f.token, input)
	require.ErrorIs(t, err, models.ErrSourceRunCapacity)
	require.Equal(t, r, f.find(t, r.UUID))
	input.RequestUUID = uuid.NewString()
	input.Window = models.SourceWindow{Since: &base, Until: from.Add(time.Minute)}
	r = f.submit(t, input)
	require.Len(t, r.Pending, 1, "covering the gaps can coalesce at capacity")
}

func TestSourceRunNewRootScopeListsOnlyAuthorizedHistory(t *testing.T) {
	f := newSourceRunFixture(t)
	oldInput := f.request()
	old := f.submit(t, oldInput)
	binding, err := archive.ProbeMediaRoot(t.TempDir())
	require.NoError(t, err)
	root := putMediaRoot(t, f.repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Moved", State: "active", Binding: binding}})
	definition := f.collection.SourceCollectionDefinition
	definition.RootUUID = &root.UUID
	f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	_, f.token, err = f.service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID, RootUUID: &root.UUID}}, nil)
	require.NoError(t, err)
	_, err = f.coordinator.Submit(t.Context(), f.token, oldInput)
	require.ErrorIs(t, err, ingest.ErrForbidden)
	_, err = f.coordinator.Find(t.Context(), f.token, old.UUID)
	require.ErrorIs(t, err, ingest.ErrForbidden)
	newRun := f.submit(t, f.request())
	rows, err := f.coordinator.List(t.Context(), f.token, f.collection.UUID, 0)
	require.NoError(t, err)
	require.Equal(t, []models.SourceRun{*newRun}, rows)
	ready, err := f.coordinator.Ready(t.Context(), f.token, root.UUID, newRun.PolicySHA256, 0)
	require.NoError(t, err)
	require.Equal(t, []models.SourceRunCandidate{{Sequence: newRun.Sequence, UUID: newRun.UUID}}, ready)
	_, err = f.coordinator.Ready(t.Context(), f.token, f.root.UUID, old.PolicySHA256, 0)
	require.ErrorIs(t, err, ingest.ErrForbidden)
}

func TestSourceRunAnonymisationRemovesWorkerAndTargetState(t *testing.T) {
	f := newSourceRunFixture(t)
	r := f.claim(t, f.submit(t, f.request()))
	require.NotNil(t, r)
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	raw := openRawDB(t, output)
	defer raw.Close()
	for _, table := range []string{"source_runs", "source_run_requests", "source_run_attempts", "source_run_cooldowns", "source_run_reviews"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.Equal(t, "running", f.find(t, r.UUID).State, "only the export is anonymised")
}

func newSourceRunFixture(t *testing.T) *sourceRunFixture {
	t.Helper()
	db, repo := archiveTestDatabase(t)
	f := &sourceRunFixture{db: db, repo: repo, service: ingest.New(repo), now: time.Now().UTC().Truncate(time.Millisecond)}
	f.coordinator = ingest.NewRunCoordinator(f.service)
	f.coordinator.Now = func() time.Time { return f.now }
	binding, err := archive.ProbeMediaRoot(t.TempDir())
	require.NoError(t, err)
	f.root = putMediaRoot(t, repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Downloads", State: "active", Binding: binding}})
	f.collection = putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Feed", Kind: "feed", State: "active", Namespace: "native:reddit", TargetURL: "https://www.reddit.com/user/example/submitted/", RootUUID: &f.root.UUID, PathPrefix: "Example"}})
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		f.producer, err = repo.Ingest.CreateProducer(ctx, "Host worker")
		return err
	}))
	f.credential, f.token, err = f.service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID, RootUUID: &f.root.UUID}}, nil)
	require.NoError(t, err)
	return f
}
func (f *sourceRunFixture) request() models.SourceRunRequest {
	since := f.now.AddDate(0, 0, -7)
	return models.SourceRunRequest{RequestUUID: uuid.NewString(), CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, Operation: "download", PolicySHA256: strings.Repeat("a", 64), Window: models.SourceWindow{Since: &since, Until: f.now}, CooldownSeconds: 5}
}
func (f *sourceRunFixture) submit(t *testing.T, input models.SourceRunRequest) *models.SourceRun {
	t.Helper()
	r, err := f.coordinator.Submit(t.Context(), f.token, input)
	require.NoError(t, err)
	return r
}
func (f *sourceRunFixture) claim(t *testing.T, r *models.SourceRun) *models.SourceRun {
	t.Helper()
	claimed, err := f.coordinator.Claim(t.Context(), f.token, r.UUID, uuid.NewString(), r.PolicySHA256, time.Minute)
	require.NoError(t, err)
	return claimed
}
func (f *sourceRunFixture) find(t *testing.T, id string) *models.SourceRun {
	t.Helper()
	r, err := f.coordinator.Find(t.Context(), f.token, id)
	require.NoError(t, err)
	return r
}
func (f *sourceRunFixture) finish(t *testing.T, r *models.SourceRun, state string) *models.SourceRun {
	t.Helper()
	outcome := models.SourceRunOutcome{State: state}
	if state != "succeeded" {
		outcome.ErrorCode = "network_unreachable"
	}
	ret, err := f.coordinator.Finish(t.Context(), f.token, r.Lease(), outcome)
	require.NoError(t, err)
	return ret
}

func TestSourceRunCoalescesWiderWindowsWithoutRepeatingCompletedWork(t *testing.T) {
	f := newSourceRunFixture(t)
	f.coordinator.MaxActive = 1
	input := f.request()
	r := f.submit(t, input)
	require.Equal(t, f.collection.TargetURL, r.TargetURL)
	require.Equal(t, f.collection.PathPrefix, r.PathPrefix)
	require.Equal(t, r, f.submit(t, input))
	changed := input
	changed.PolicySHA256 = strings.Repeat("b", 64)
	_, err := f.coordinator.Submit(t.Context(), f.token, changed)
	require.ErrorIs(t, err, models.ErrSourceRunConflict)
	changed.RequestUUID = uuid.NewString()
	_, err = f.coordinator.Submit(t.Context(), f.token, changed)
	require.ErrorIs(t, err, models.ErrSourceRunCapacity)
	active := f.claim(t, r)
	require.NotNil(t, active)
	f.now = f.now.Add(10 * time.Second)
	full := input
	full.RequestUUID = uuid.NewString()
	full.Window = models.SourceWindow{Until: f.now}
	merged := f.submit(t, full)
	require.Equal(t, r.UUID, merged.UUID)
	require.Len(t, merged.Pending, 2)
	require.Nil(t, merged.Pending[0].Since)
	require.Equal(t, *input.Window.Since, merged.Pending[0].Until)
	require.Equal(t, input.Window.Until, *merged.Pending[1].Since)
	require.Equal(t, f.now, merged.Pending[1].Until)
	require.Nil(t, f.claim(t, r), "a second worker must not join the active traversal")
	done := f.finish(t, active, "succeeded")
	require.Equal(t, "queued", done.State)
	require.Equal(t, []models.SourceWindow{input.Window}, done.Completed)
	require.Nil(t, f.claim(t, r), "cooldown is retained between ranges")
	f.now = done.AvailableAt
	next := f.claim(t, r)
	require.Equal(t, merged.Pending[1], *next.Window, "new captures precede old history")
	done = f.finish(t, next, "succeeded")
	f.now = done.AvailableAt
	next = f.claim(t, r)
	require.Equal(t, merged.Pending[0], *next.Window)
	done = f.finish(t, next, "succeeded")
	require.Equal(t, "succeeded", done.State)
	require.Empty(t, done.Pending)
	require.Equal(t, []models.SourceWindow{full.Window}, done.Completed)
	require.Equal(t, done, f.submit(t, full), "completed request receipts never create a new run")
	input.RequestUUID = uuid.NewString()
	fresh := f.submit(t, input)
	require.NotEqual(t, done.UUID, fresh.UUID, "a new scheduled request may observe the source again")
}

func TestSourceRunResponseKeepsPinnedCollectionDestination(t *testing.T) {
	f := newSourceRunFixture(t)
	r := f.submit(t, f.request())
	definition := f.collection.SourceCollectionDefinition
	definition.TargetURL, definition.PathPrefix = "https://www.reddit.com/user/other/submitted/", "Other"
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID,
		ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
	restored := f.find(t, r.UUID)
	require.Equal(t, r.TargetURL, restored.TargetURL)
	require.Equal(t, r.PathPrefix, restored.PathPrefix)
	require.Equal(t, r.CollectionRevision, restored.CollectionRevision)
}

func TestSourceRunConcurrentRequestsAndClaimsHaveOneOwner(t *testing.T) {
	f := newSourceRunFixture(t)
	input := f.request()
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := []string{}
	for range 8 {
		wg.Go(func() {
			r, err := f.coordinator.Submit(t.Context(), f.token, input)
			require.NoError(t, err)
			mu.Lock()
			ids = append(ids, r.UUID)
			mu.Unlock()
		})
	}
	wg.Wait()
	for _, id := range ids {
		require.Equal(t, ids[0], id)
	}
	claims := []*models.SourceRun{}
	for range 8 {
		wg.Go(func() {
			r, err := f.coordinator.Claim(t.Context(), f.token, ids[0], uuid.NewString(), input.PolicySHA256, time.Minute)
			require.NoError(t, err)
			if r != nil {
				mu.Lock()
				claims = append(claims, r)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	require.Len(t, claims, 1)
	replay, err := f.coordinator.Claim(t.Context(), f.token, ids[0], claims[0].OwnerUUID, input.PolicySHA256, time.Minute)
	require.NoError(t, err)
	require.Equal(t, claims[0], replay, "lost claim responses replay the same fence")
	attempts, err := f.coordinator.Attempts(t.Context(), f.token, ids[0], 0)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
}

func TestSourceRunRestartRecoveryCheckpointsAndTokenRotation(t *testing.T) {
	f := newSourceRunFixture(t)
	r := f.submit(t, f.request())
	first := f.claim(t, r)
	progress := models.SourceRunProgress{ItemsSeen: 17, FilesCompleted: 11, Cursor: "post_17"}
	_, err := f.coordinator.Progress(t.Context(), f.token, first.Lease(), progress)
	require.NoError(t, err)
	bad := progress
	bad.Cursor = "post_16"
	_, err = f.coordinator.Progress(t.Context(), f.token, first.Lease(), bad)
	require.ErrorIs(t, err, models.ErrSourceRunConflict)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.now = *first.LeaseUntil
	_, err = f.coordinator.Renew(t.Context(), f.token, first.Lease(), time.Minute)
	require.ErrorIs(t, err, models.ErrSourceRunLease)
	require.Nil(t, f.claim(t, r), "recovery preserves retry backoff")
	recovered := f.find(t, r.UUID)
	require.Equal(t, "queued", recovered.State)
	require.Equal(t, 1, recovered.Failures)
	f.now = recovered.AvailableAt
	second := f.claim(t, r)
	require.EqualValues(t, 2, second.Fence)
	require.Equal(t, progress, second.Progress)
	_, err = f.coordinator.Finish(t.Context(), f.token, first.Lease(), models.SourceRunOutcome{State: "succeeded"})
	require.ErrorIs(t, err, models.ErrSourceRunLease)
	_, err = f.coordinator.Progress(t.Context(), f.token, first.Lease(), progress)
	require.ErrorIs(t, err, models.ErrSourceRunLease)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.Ingest.RevokeCredential(ctx, f.credential.UUID) }))
	_, err = f.coordinator.Renew(t.Context(), f.token, second.Lease(), time.Minute)
	require.ErrorIs(t, err, ingest.ErrUnauthorized)
	_, f.token, err = f.service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID, RootUUID: &f.root.UUID}}, nil)
	require.NoError(t, err)
	renewed, err := f.coordinator.Renew(t.Context(), f.token, second.Lease(), time.Minute)
	require.NoError(t, err)
	require.Equal(t, second.Lease(), renewed.Lease())
	require.Equal(t, "succeeded", f.finish(t, second, "succeeded").State)
	attempts, err := f.coordinator.Attempts(t.Context(), f.token, r.UUID, 0)
	require.NoError(t, err)
	require.Len(t, attempts, 2)
	require.Equal(t, "expired", attempts[0].Outcome)
	require.Equal(t, "succeeded", attempts[1].Outcome)
	require.Equal(t, progress, attempts[0].Progress)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceRunRetryLimitsDeferralsAndReviewCannotBeBypassedByTimers(t *testing.T) {
	f := newSourceRunFixture(t)
	input := f.request()
	r := f.submit(t, input)
	for attempt := 1; attempt <= 8; attempt++ {
		active := f.claim(t, r)
		require.NotNil(t, active)
		r = f.finish(t, active, "retry")
		require.Equal(t, attempt, r.Failures)
		repeat := input
		repeat.RequestUUID = uuid.NewString()
		coalesced := f.submit(t, repeat)
		require.Equal(t, r.AvailableAt, coalesced.AvailableAt)
		require.Equal(t, r.UUID, coalesced.UUID)
		require.Nil(t, f.claim(t, r))
		f.now = r.AvailableAt
	}
	require.Equal(t, "deferred", r.State)
	require.Nil(t, f.claim(t, r))
	input.RequestUUID = uuid.NewString()
	input.Window.Since = nil
	r = f.submit(t, input)
	require.Equal(t, "deferred", r.State)
	require.Nil(t, r.Pending[0].Since)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceRun.Review(ctx, r.UUID, r.Revision-1, "retry", f.now)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceRunConflict)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceRun.Review(ctx, r.UUID, r.Revision, "retry", f.now)
		return err
	}))
	active := f.claim(t, r)
	require.NotNil(t, active)
	require.Equal(t, 0, active.Failures)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceRun.Review(ctx, r.UUID, active.Revision, "cancel", f.now)
		return err
	}))
	_, err = f.coordinator.Finish(t.Context(), f.token, active.Lease(), models.SourceRunOutcome{State: "succeeded"})
	require.ErrorIs(t, err, models.ErrSourceRunLease)
	require.Equal(t, "cancelled", f.find(t, r.UUID).State)
}

func TestSourceRunDestinationExclusionAndDefinitionChanges(t *testing.T) {
	f := newSourceRunFixture(t)
	first := f.submit(t, f.request())
	active := f.claim(t, first)
	makeOther := func(prefix, target string) *models.SourceRun {
		c := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Other", Kind: "feed", State: "active", Namespace: "native:reddit", TargetURL: target, RootUUID: &f.root.UUID, PathPrefix: prefix}})
		_, token, err := f.service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: c.UUID, RootUUID: &f.root.UUID}, {CollectionUUID: f.collection.UUID, RootUUID: &f.root.UUID}}, nil)
		require.NoError(t, err)
		f.token = token
		input := f.request()
		input.CollectionUUID = c.UUID
		input.CollectionRevision = c.Revision
		return f.submit(t, input)
	}
	child := makeOther("Example/child", "https://www.reddit.com/user/child/submitted/")
	require.Nil(t, f.claim(t, child))
	sameURL := makeOther("elsewhere", f.collection.TargetURL)
	require.Nil(t, f.claim(t, sameURL))
	different := makeOther("ExampleTwo", "https://www.reddit.com/user/two/submitted/")
	require.NotNil(t, f.claim(t, different))
	f.root = putMediaRoot(t, f.repo, models.MediaRootInput{UUID: f.root.UUID, ExpectedRevision: f.root.Revision, Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Downloads", State: "disabled", Binding: f.root.Binding}})
	_, err := f.coordinator.Renew(t.Context(), f.token, active.Lease(), time.Minute)
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
	_, err = f.coordinator.Finish(t.Context(), f.token, active.Lease(), models.SourceRunOutcome{State: "succeeded"})
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
	f.finish(t, active, "deferred")
	// Historical request receipt remains readable after disabling its root.
	require.Equal(t, "deferred", f.find(t, first.UUID).State)
}
