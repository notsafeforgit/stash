package sqlite_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceRunInitialAndIncrementalQueuesShareSite(t *testing.T) {
	for _, site := range []string{"reddit", "twitter"} {
		t.Run(site, func(t *testing.T) {
			f := newSourceRunFixture(t)
			var initialProducer *models.IngestProducer
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				var err error
				initialProducer, err = f.repo.Ingest.CreateProducer(ctx, "n8n initial profiles")
				return err
			}))
			_, hostToken, err := f.service.IssueCredential(t.Context(), f.producer.UUID, nil, nil, f.root.UUID)
			require.NoError(t, err)
			_, initialToken, err := f.service.IssueCredential(t.Context(), initialProducer.UUID, nil, nil, f.root.UUID)
			require.NoError(t, err)
			f.token = hostToken
			makeRun := func(name, token string, full bool) *models.SourceRun {
				target := "https://www.reddit.com/user/" + name + "/"
				if site == "twitter" {
					target = "https://x.com/" + name
				}
				collection := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review",
					SourceCollectionDefinition: models.SourceCollectionDefinition{Label: name, Kind: "account", State: "active",
						Namespace: "native:" + site, TargetURL: target, RootUUID: &f.root.UUID, PathPrefix: "."}})
				request := f.request()
				request.CollectionUUID, request.CollectionRevision = collection.UUID, collection.Revision
				// Twitter's incremental archive-stop traversal also has no lower
				// date bound. A missing since must not identify an initial scrape.
				if full || site == "twitter" {
					request.Window.Since = nil
				}
				if full {
					request.PolicySHA256 = strings.Repeat("b", 64)
				}
				run, err := f.coordinator.Submit(t.Context(), token, request)
				require.NoError(t, err)
				return run
			}
			incremental := makeRun("existing", hostToken, false)
			initial := makeRun("newprofile", initialToken, true)
			nextInitial := makeRun("nextprofile", initialToken, true)
			nextIncremental := makeRun("otherexisting", hostToken, false)
			claim := func(token string, run *models.SourceRun) *models.SourceRun {
				result, err := f.coordinator.Claim(t.Context(), token, run.UUID, uuid.NewString(), run.PolicySHA256, time.Minute)
				require.NoError(t, err)
				return result
			}
			start := make(chan struct{})
			var group sync.WaitGroup
			running := make([]*models.SourceRun, 2)
			errs := make([]error, 2)
			for i, work := range []struct {
				token string
				run   *models.SourceRun
			}{{hostToken, incremental}, {initialToken, initial}} {
				group.Go(func() {
					<-start
					running[i], errs[i] = f.coordinator.Claim(t.Context(), work.token, work.run.UUID, uuid.NewString(), work.run.PolicySHA256, time.Minute)
				})
			}
			close(start)
			group.Wait()
			for i := range running {
				require.NoError(t, errs[i])
				require.NotNil(t, running[i], "the site's initial and incremental queues must both progress")
			}
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(path))
			require.Nil(t, claim(initialToken, nextInitial), "only one initial scrape per site/root")
			require.Nil(t, claim(hostToken, nextIncremental), "incremental workers do not stack up")
			require.Nil(t, claim(initialToken, incremental), "a second producer cannot join the same run")
			_, err = f.coordinator.Finish(t.Context(), initialToken, running[1].Lease(), models.SourceRunOutcome{State: "succeeded"})
			require.NoError(t, err)
			sameProfile := f.request()
			sameProfile.CollectionUUID, sameProfile.CollectionRevision = incremental.CollectionUUID, incremental.CollectionRevision
			sameProfile.PolicySHA256, sameProfile.Window.Since = strings.Repeat("b", 64), nil
			duplicate, err := f.coordinator.Submit(t.Context(), initialToken, sameProfile)
			require.NoError(t, err)
			require.Nil(t, claim(initialToken, duplicate), "different policies/windows cannot bypass the same-profile fence")
			next := claim(initialToken, nextInitial)
			require.NotNil(t, next, "initial queue advances while the incremental run is still active")
			require.Equal(t, running[0], f.find(t, incremental.UUID), "restart and other queue must preserve the current traversal")
			_, err = f.coordinator.Finish(t.Context(), initialToken, next.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "rate_limited"})
			require.NoError(t, err)
			f.finish(t, running[0], "succeeded")
			require.Nil(t, claim(hostToken, nextIncremental), "site rate limits still apply across both queues")
		})
	}
}
