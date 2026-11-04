package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func profileBindingFixture(t *testing.T) (*sourceRunFixture, *models.ArchiveEntity) {
	t.Helper()
	f := newSourceRunFixture(t)
	definition := f.collection.SourceCollectionDefinition
	definition.Kind, definition.Namespace, definition.TargetURL = "account", "native:redgifs", "https://www.redgifs.com/users/example"
	f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID,
		ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
	editProfileURLs(t, f.repo, 71, models.RelationshipUpdateModeAdd, definition.TargetURL)
	return f, archiveFind(t, f.repo, models.ArchivePerformer, 71)
}

func linkProfile(t *testing.T, f *sourceRunFixture, owner *models.ArchiveEntity) *models.ProfileAccountLinkResult {
	t.Helper()
	var result *models.ProfileAccountLinkResult
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.SourceCollection.LinkProfileAccount(ctx, f.collection.UUID, models.ProfileAccountLink{
			ExpectedRevision: f.collection.Revision, PerformerUUID: owner.UUID})
		return err
	}))
	return result
}

func TestLinkProfileAccountPreservesWorkPolicyMonitoringAndReplay(t *testing.T) {
	f, owner := profileBindingFixture(t)
	var policy *models.MetadataPolicy
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		policy, err = f.repo.MetadataPolicy.Put(ctx, models.MetadataPolicyInput{CollectionUUID: f.collection.UUID,
			ExpectedCollectionRevision: f.collection.Revision, Origin: "review", Definition: models.MetadataPolicyDefinition{
				Enabled: true, Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{models.ArchiveScene: {
					OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{"title": {JQ: ".source.metadata.title"}},
				}},
			}})
		return err
	}))
	run := f.submit(t, f.request())
	before := linkProfile(t, f, owner)
	require.Equal(t, "waiting", before.State)
	require.Equal(t, f.collection, before.Collection)
	claimed := f.claim(t, run)
	require.Equal(t, "waiting", linkProfile(t, f, owner).State)
	finished := f.finish(t, claimed, "succeeded")
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceSubscription.Put(ctx, f.collection.UUID, models.SourceSubscriptionInput{Enabled: false})
		return err
	}))
	result := linkProfile(t, f, owner)
	require.Equal(t, "linked", result.State)
	require.NotNil(t, result.Collection.AccountUUID)
	require.Equal(t, f.collection.Revision+1, result.Collection.Revision)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, result, linkProfile(t, f, owner), "lost response can retry the original revision")
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		linked, err := f.repo.SourceAccount.Ownership(ctx, *result.Collection.AccountUUID)
		require.NoError(t, err)
		require.Equal(t, owner.UUID, *linked.PerformerUUID)
		current, err := f.repo.MetadataPolicy.Find(ctx, f.collection.UUID)
		require.NoError(t, err)
		require.Equal(t, policy.Definition, current.Definition)
		require.Equal(t, result.Collection.Revision, current.CollectionRevision)
		require.Equal(t, policy.Revision+1, current.Revision)
		page, err := f.repo.SourceSubscription.List(ctx, models.SourceSubscriptionFilter{CollectionUUID: f.collection.UUID, Limit: 10})
		require.NoError(t, err)
		require.Len(t, page.Items, 1)
		require.False(t, page.Items[0].Enabled)
		return nil
	}))
	require.Equal(t, finished, f.find(t, finished.UUID), "completed crawl coverage is retained unchanged")
}

func TestLinkProfileAccountPreservesConflictingChoices(t *testing.T) {
	for _, scenario := range []string{"ambiguous-url", "missing-url", "unlinked", "different-owner", "ambiguous-account", "translation-policy"} {
		t.Run(scenario, func(t *testing.T) {
			f, owner := profileBindingFixture(t)
			switch scenario {
			case "ambiguous-url":
				editProfileURLs(t, f.repo, 72, models.RelationshipUpdateModeAdd, f.collection.TargetURL)
			case "missing-url":
				editProfileURLs(t, f.repo, 71, models.RelationshipUpdateModeSet)
			case "translation-policy":
				putTranslationPolicy(t, f.repo, f.collection, 0, translationPolicyDefinition())
			default:
				first := linkProfile(t, f, owner)
				f.collection = first.Collection
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					account, err := f.repo.SourceAccount.Find(ctx, *f.collection.AccountUUID)
					require.NoError(t, err)
					input := models.AccountOwnershipInput{AccountUUID: account.UUID, ExpectedAccountRevision: account.Revision,
						State: models.AccountOwnershipUnlinked, Origin: "review"}
					if scenario == "different-owner" {
						other, err := f.repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, 72)
						require.NoError(t, err)
						input.State, input.PerformerUUID, input.ExpectedPerformerRevision = models.AccountOwnershipLinked, other.UUID, other.Revision
					}
					_, err = f.repo.SourceAccount.DecideOwnership(ctx, input)
					return err
				}))
				if scenario == "ambiguous-account" {
					other := createSourceAccount(t, f.repo, "native:redgifs")
					require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
						_, err := f.repo.SourceAccount.ObserveIdentifier(ctx, other.UUID, models.AccountReference{Namespace: "native:redgifs", Kind: "handle", Value: "example"},
							models.AccountIdentifierEvidence{Key: "reused", Basis: "profile-url", Origin: "fixture", FirstObserved: f.now, LastObserved: f.now})
						return err
					}))
				}
			}
			result := linkProfile(t, f, owner)
			require.Equal(t, "review", result.State)
			require.Equal(t, f.collection, result.Collection)
		})
	}
}
