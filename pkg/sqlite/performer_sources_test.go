package sqlite_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func performerAccounts(t *testing.T, repo models.Repository, id, after string, limit int) *models.PerformerSourceAccounts {
	t.Helper()
	var result *models.PerformerSourceAccounts
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = repo.SourceAccount.PerformerAccounts(ctx, id, after, limit)
		return err
	}))
	require.NotNil(t, result)
	return result
}

func performerIdentities(t *testing.T, repo models.Repository, id, after string, limit int) *models.PerformerSourceIdentities {
	t.Helper()
	var result *models.PerformerSourceIdentities
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = repo.SourceAccount.PerformerIdentities(ctx, id, after, limit)
		return err
	}))
	require.NotNil(t, result)
	return result
}

func TestPerformerSourcesFollowMergesAdoptionAndCurrentAccountChoices(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	source := archiveFind(t, repo, models.ArchivePerformer, 71)
	target := archiveFind(t, repo, models.ArchivePerformer, 72)
	reddit := createSourceAccount(t, repo, "native:reddit")
	twitter := createSourceAccount(t, repo, "native:twitter")
	rejected := createSourceAccount(t, repo, "native:reddit")
	unowned := createSourceAccount(t, repo, "native:reddit")
	for _, link := range []struct {
		account string
		owner   int
	}{{reddit.UUID, 71}, {twitter.UUID, 72}, {rejected.UUID, 71}, {rejected.UUID, 0}} {
		_, _, err := applyAccountReview(repo, accountReviewRequest(t, repo, accountReviewInput(t, repo, link.account, link.owner)))
		require.NoError(t, err)
	}
	require.Len(t, performerAccounts(t, repo, source.UUID, "", 25).Accounts, 1)
	require.Len(t, performerAccounts(t, repo, target.UUID, "", 25).Accounts, 1)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Merge(ctx, []int{71}, 72) }))
	want := []string{reddit.UUID, twitter.UUID}
	slices.Sort(want)
	first := performerAccounts(t, repo, source.UUID, "", 1)
	require.Equal(t, source.UUID, first.RequestedUUID)
	require.Equal(t, target.UUID, first.Performer.UUID)
	require.Equal(t, 72, *first.Performer.LocalID)
	require.Len(t, first.Accounts, 1)
	require.Equal(t, want[0], first.Accounts[0].UUID)
	second := performerAccounts(t, repo, source.UUID, want[0], 1)
	require.Len(t, second.Accounts, 1)
	require.Equal(t, want[1], second.Accounts[0].UUID)
	require.Empty(t, performerAccounts(t, repo, target.UUID, want[1], 1).Accounts)
	for _, page := range []*models.PerformerSourceAccounts{first, second} {
		for _, account := range page.Accounts {
			require.NotEqual(t, rejected.UUID, account.UUID)
			require.NotEqual(t, unowned.UUID, account.UUID)
			require.Equal(t, target.UUID, account.Ownership.Performer.UUID)
		}
	}
	adopted := uuid.NewString()
	current := archiveFind(t, repo, models.ArchivePerformer, 72)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, current.UUID, adopted, current.Revision)
		return err
	}))
	identities := performerIdentities(t, repo, source.UUID, "", 25)
	require.Equal(t, adopted, identities.Performer.UUID)
	require.Len(t, identities.Identities, 3)
	var ids []string
	for _, identity := range identities.Identities {
		ids = append(ids, identity.UUID)
		if identity.UUID == adopted {
			require.Equal(t, models.ArchiveEntityActive, identity.State)
			require.Nil(t, identity.RetiredAt)
			require.Nil(t, identity.RedirectTo)
		} else {
			require.Equal(t, models.ArchiveEntityRedirected, identity.State)
			require.NotNil(t, identity.RedirectTo)
			require.NotNil(t, identity.RetiredAt)
		}
	}
	require.ElementsMatch(t, []string{source.UUID, target.UUID, adopted}, ids)
	require.True(t, slices.IsSorted(ids))
	for n, id := range ids {
		page := performerIdentities(t, repo, target.UUID, "", n+1)
		require.Len(t, page.Identities, n+1)
		require.Equal(t, id, page.Identities[n].UUID)
	}
	require.Empty(t, performerIdentities(t, repo, target.UUID, ids[2], 25).Identities)
	// Consolidation preserves the selected owner but returns the survivor once.
	destination := createSourceAccount(t, repo, reddit.Namespace)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		preview, err := repo.SourceAccount.PreviewConsolidation(ctx, reddit.UUID, destination.UUID)
		if err != nil {
			return err
		}
		_, err = repo.SourceAccount.Consolidate(ctx, models.AccountConsolidationInput{SourceUUID: reddit.UUID,
			DestinationUUID: destination.UUID, Signature: preview.Signature, OwnershipMode: "preserve", Origin: "review"})
		return err
	}))
	linked := performerAccounts(t, repo, target.UUID, "", 25)
	require.Len(t, linked.Accounts, 2)
	var accountIDs []string
	for _, account := range linked.Accounts {
		accountIDs = append(accountIDs, account.UUID)
		require.Equal(t, account.UUID, account.CanonicalUUID)
		require.Nil(t, account.RedirectTo)
	}
	require.ElementsMatch(t, []string{destination.UUID, twitter.UUID}, accountIDs)
	_, _, err := applyAccountReview(repo, accountReviewRequest(t, repo, accountReviewInput(t, repo, destination.UUID, 0)))
	require.NoError(t, err)
	linked = performerAccounts(t, repo, source.UUID, "", 25)
	require.Len(t, linked.Accounts, 1)
	require.Equal(t, twitter.UUID, linked.Accounts[0].UUID)
	// A retained UUID cannot link to an unrelated reused local performer ID.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Destroy(ctx, 72) }))
	attachmentSQL(t, db, `INSERT INTO performers(id,created_at,updated_at) VALUES(72,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO performer_names(performer_id,name,position) VALUES(72,'New performer',0)`)
	retired := performerAccounts(t, repo, source.UUID, "", 25)
	require.Equal(t, models.ArchiveEntityDeleted, retired.Performer.State)
	require.Nil(t, retired.Performer.LocalID)
	require.Empty(t, retired.Performer.Name)
	require.Len(t, retired.Accounts, 1)
	newIdentity := archiveFind(t, repo, models.ArchivePerformer, 72)
	require.NotEqual(t, adopted, newIdentity.UUID)
	require.Empty(t, performerAccounts(t, repo, newIdentity.UUID, "", 25).Accounts)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.Equal(t, retired, performerAccounts(t, repo, source.UUID, "", 25))
}

func TestPerformerSourcesRejectInvalidAndOversizedIdentityScopes(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	performer := archiveFind(t, repo, models.ArchivePerformer, 71)
	scene := archiveFind(t, repo, models.ArchiveScene, 31)
	for _, method := range []func(context.Context, string, string, int) error{
		func(ctx context.Context, id, after string, limit int) error {
			_, err := repo.SourceAccount.PerformerAccounts(ctx, id, after, limit)
			return err
		},
		func(ctx context.Context, id, after string, limit int) error {
			_, err := repo.SourceAccount.PerformerIdentities(ctx, id, after, limit)
			return err
		},
	} {
		for _, input := range []struct {
			id, after string
			limit     int
		}{{"invalid", "", 25}, {scene.UUID, "", 25}, {performer.UUID, "bad", 25}, {performer.UUID, "", 0}, {performer.UUID, "", 101}} {
			err := repo.WithReadTxn(t.Context(), func(ctx context.Context) error { return method(ctx, input.id, input.after, input.limit) })
			require.ErrorIs(t, err, models.ErrAccountReviewInvalid)
		}
	}
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		accounts, err := repo.SourceAccount.PerformerAccounts(ctx, uuid.NewString(), "", 25)
		require.NoError(t, err)
		require.Nil(t, accounts)
		identities, err := repo.SourceAccount.PerformerIdentities(ctx, uuid.NewString(), "", 25)
		require.NoError(t, err)
		require.Nil(t, identities)
		return nil
	}))
	attachmentSQL(t, db, `WITH RECURSIVE numbers(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n<1024)
INSERT INTO archive_entities(uuid,kind,state,redirect_to,created_at,retired_at)
SELECT printf('12345678-1234-4234-8234-%012d',n),'performer','redirected',?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP FROM numbers`, performer.UUID)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceAccount.PerformerAccounts(ctx, performer.UUID, "", 25)
		require.ErrorIs(t, err, models.ErrPerformerSourceLimit)
		_, err = repo.SourceAccount.PerformerIdentities(ctx, performer.UUID, "", 25)
		require.ErrorIs(t, err, models.ErrPerformerSourceLimit)
		return nil
	}))
}
