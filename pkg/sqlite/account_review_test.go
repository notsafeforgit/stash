package sqlite_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func accountReviewState(t *testing.T, repo models.Repository, id string) *models.AccountReviewState {
	t.Helper()
	var ret *models.AccountReviewState
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceAccount.ReviewAccount(ctx, id)
		return err
	}))
	require.NotNil(t, ret)
	return ret
}

func accountReviewInput(t *testing.T, repo models.Repository, id string, performer int) models.AccountOwnershipReviewInput {
	t.Helper()
	account := accountReviewState(t, repo, id)
	ret := models.AccountOwnershipReviewInput{AccountUUID: id, AccountRevision: account.Revision, State: models.AccountOwnershipUnlinked}
	if performer != 0 {
		entity := archiveFind(t, repo, models.ArchivePerformer, performer)
		ret.State, ret.PerformerUUID, ret.PerformerRevision = models.AccountOwnershipLinked, entity.UUID, entity.Revision
	}
	return ret
}

func accountReviewRequest(t *testing.T, repo models.Repository, input models.AccountOwnershipReviewInput) models.AccountOwnershipReviewApplyInput {
	t.Helper()
	var preview *models.AccountOwnershipPreview
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		preview, err = repo.SourceAccount.PreviewOwnership(ctx, input)
		return err
	}))
	return models.AccountOwnershipReviewApplyInput{AccountOwnershipReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
}

func applyAccountReview(repo models.Repository, input models.AccountOwnershipReviewApplyInput) (*models.AccountOwnershipReview, bool, error) {
	var result *models.AccountOwnershipReview
	var replayed bool
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		result, replayed, err = repo.SourceAccount.ApplyOwnershipReview(ctx, input)
		return err
	})
	return result, replayed, err
}

func TestAccountReviewReplayPreservesExplicitChoicesAndDepictedMetadata(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:reddit")
	input := accountReviewInput(t, repo, account.UUID, 72)
	input.Reason = "Confirmed account owner"
	request := accountReviewRequest(t, repo, input)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	before := albumJobRows(t, raw, "performers_scenes")
	first, replayed, err := applyAccountReview(repo, request)
	require.NoError(t, err)
	require.False(t, replayed)
	require.Equal(t, before, albumJobRows(t, raw, "performers_scenes"), "an account owner is not depicted attribution")
	state := accountReviewState(t, repo, account.UUID)
	require.Equal(t, 72, *state.Ownership.Performer.LocalID)
	require.Equal(t, "Shared name", state.Ownership.Performer.Name)
	second, replayed, err := applyAccountReview(repo, request)
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, first, second)
	changed := request
	changed.Reason = "Different request"
	_, _, err = applyAccountReview(repo, changed)
	require.ErrorIs(t, err, models.ErrAccountReviewReplay)
	unlink := accountReviewRequest(t, repo, accountReviewInput(t, repo, account.UUID, 0))
	_, _, err = applyAccountReview(repo, unlink)
	require.NoError(t, err)
	require.Equal(t, models.AccountOwnershipUnlinked, accountReviewState(t, repo, account.UUID).Ownership.State)
	third, replayed, err := applyAccountReview(repo, request)
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, first, third, "recovery returns the original receipt after a later unlink")
	require.Equal(t, models.AccountOwnershipUnlinked, accountReviewState(t, repo, account.UUID).Ownership.State)
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM account_performer_decisions"))
	require.NoError(t, raw.Close())
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	_, replayed, err = applyAccountReview(repo, request)
	require.NoError(t, err)
	require.True(t, replayed)
}

func TestAccountReviewRejectsChangedTargetsAndCurrentOwner(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:twitter")
	request := accountReviewRequest(t, repo, accountReviewInput(t, repo, account.UUID, 71))
	observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "id", Value: "123"}, accountEvidence())
	_, _, err := applyAccountReview(repo, request)
	require.ErrorIs(t, err, models.ErrSourceAccountConflict)
	request = accountReviewRequest(t, repo, accountReviewInput(t, repo, account.UUID, 71))
	attachmentSQL(t, db, "UPDATE performer_names SET name='New canonical name' WHERE performer_id=71 AND position=0")
	_, _, err = applyAccountReview(repo, request)
	require.ErrorIs(t, err, models.ErrSourceAccountConflict)
	request = accountReviewRequest(t, repo, accountReviewInput(t, repo, account.UUID, 71))
	_, _, err = applyAccountReview(repo, request)
	require.NoError(t, err)
	unlink := accountReviewRequest(t, repo, accountReviewInput(t, repo, account.UUID, 0))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Merge(ctx, []int{71}, 72) }))
	_, _, err = applyAccountReview(repo, unlink)
	require.ErrorIs(t, err, models.ErrSourceAccountConflict, "unlink must re-review an owner that merged after preview")
	state := accountReviewState(t, repo, account.UUID)
	require.Equal(t, 72, *state.Ownership.Performer.LocalID)
	_, _, err = applyAccountReview(repo, accountReviewRequest(t, repo, accountReviewInput(t, repo, account.UUID, 0)))
	require.NoError(t, err)
}

func TestAccountReviewReceiptsSurviveIdentityChangesAndAccountConsolidation(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:instagram")
	request := accountReviewRequest(t, repo, accountReviewInput(t, repo, account.UUID, 71))
	first, _, err := applyAccountReview(repo, request)
	require.NoError(t, err)
	identity := archiveFind(t, repo, models.ArchivePerformer, 71)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, identity.UUID, uuid.NewString(), identity.Revision)
		return err
	}))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Merge(ctx, []int{71}, 72) }))
	destination := createSourceAccount(t, repo, account.Namespace)
	stale := accountReviewRequest(t, repo, accountReviewInput(t, repo, account.UUID, 0))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		preview, err := repo.SourceAccount.PreviewConsolidation(ctx, account.UUID, destination.UUID)
		if err != nil {
			return err
		}
		_, err = repo.SourceAccount.Consolidate(ctx, models.AccountConsolidationInput{SourceUUID: account.UUID, DestinationUUID: destination.UUID,
			Signature: preview.Signature, OwnershipMode: "preserve", Origin: "review"})
		return err
	}))
	_, _, err = applyAccountReview(repo, stale)
	require.ErrorIs(t, err, models.ErrSourceAccountConflict)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Destroy(ctx, 72) }))
	ret, replayed, err := applyAccountReview(repo, request)
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, first, ret)
	state := accountReviewState(t, repo, account.UUID)
	require.Equal(t, destination.UUID, state.CanonicalUUID)
	require.Equal(t, models.ArchiveEntityDeleted, state.Ownership.Performer.State)
	require.Nil(t, state.Ownership.Performer.LocalID)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	_, replayed, err = applyAccountReview(repo, request)
	require.NoError(t, err)
	require.True(t, replayed)
}

func TestAccountReviewLateFailureCannotCommitAnUnreceiptedLink(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:tiktok")
	request := accountReviewRequest(t, repo, accountReviewInput(t, repo, account.UUID, 71))
	attachmentSQL(t, db, `CREATE TRIGGER reject_account_review BEFORE INSERT ON account_ownership_reviews BEGIN SELECT RAISE(ABORT,'receipt failure'); END`)
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := repo.SourceAccount.ApplyOwnershipReview(ctx, request)
		require.ErrorContains(t, err, "receipt failure")
		return nil
	})
	require.ErrorIs(t, err, models.ErrAccountReviewInvalid)
	state := accountReviewState(t, repo, account.UUID)
	require.Equal(t, account.Revision, state.Revision)
	require.Nil(t, state.Ownership)
}

func TestAccountReviewDiscoveryRetainsAmbiguityAndUsesLiteralSearch(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	one, two := createSourceAccount(t, repo, "native:reddit"), createSourceAccount(t, repo, "native:reddit")
	for _, account := range []*models.SourceAccount{one, two} {
		observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "handle", Value: "shared"}, accountEvidence())
	}
	observeAccount(t, repo, one.UUID, models.AccountReference{Namespace: one.Namespace, Kind: "id", Value: "t2_id"}, accountEvidence())
	_, _, err := applyAccountReview(repo, accountReviewRequest(t, repo, accountReviewInput(t, repo, one.UUID, 71)))
	require.NoError(t, err)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceAccount.Create(ctx, "mirror:coomer:onlyfans", "100% known")
		return err
	}))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := repo.SourceAccount.ReviewAccounts(ctx, models.AccountReviewFilter{Query: "shared", Limit: 1})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		next, err := repo.SourceAccount.ReviewAccounts(ctx, models.AccountReviewFilter{Query: "shared", After: rows[0].UUID, Limit: 1})
		require.NoError(t, err)
		require.Len(t, next, 1)
		require.NotEqual(t, rows[0].UUID, next[0].UUID)
		rows, err = repo.SourceAccount.ReviewAccounts(ctx, models.AccountReviewFilter{Ownership: models.AccountOwnershipLinked})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, one.UUID, rows[0].UUID)
		require.Len(t, rows[0].Identifiers, 2, "handle and ID are identifiers of this account, not duplicate cards")
		rows, err = repo.SourceAccount.ReviewAccounts(ctx, models.AccountReviewFilter{Query: "%"})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "mirror:coomer:onlyfans", rows[0].Namespace)
		rows, err = repo.SourceAccount.ReviewAccounts(ctx, models.AccountReviewFilter{Ownership: models.AccountOwnershipUndecided, Namespace: "native:reddit"})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, two.UUID, rows[0].UUID)
		return nil
	}))
}

func removeAccountReviewSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec("DROP TABLE account_ownership_reviews; DELETE FROM native_migration_history WHERE version=1000068")
	require.NoError(t, err)
}

func TestAccountReviewMigrationPreservesExistingLinksAndAnonymisesReceipts(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:bluesky")
	identity := archiveFind(t, repo, models.ArchivePerformer, 71)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceAccount.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: account.UUID, ExpectedAccountRevision: account.Revision,
			State: models.AccountOwnershipLinked, PerformerUUID: identity.UUID, ExpectedPerformerRevision: identity.Revision, Origin: "review"})
		return err
	}))
	require.NoError(t, db.Close())
	raw := openRawDB(t, db.DatabasePath())
	before := albumJobRows(t, raw, "account_performer_decisions")
	removeAccountReviewSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000067,dirty=0")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, db.Open(db.DatabasePath()), &needed)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	raw = openRawDB(t, db.DatabasePath())
	require.Equal(t, before, albumJobRows(t, raw, "account_performer_decisions"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM account_ownership_reviews"))
	require.NoError(t, raw.Close())
	_, _, err = applyAccountReview(repo, accountReviewRequest(t, repo, accountReviewInput(t, repo, account.UUID, 0)))
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(db, path)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	raw = openRawDB(t, path)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM account_ownership_reviews"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestAccountReviewCorruptReceiptIsRejectedBeforeWrites(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "mirror:kemono:patreon")
	_, _, err := applyAccountReview(repo, accountReviewRequest(t, repo, accountReviewInput(t, repo, account.UUID, 71)))
	require.NoError(t, err)
	require.NoError(t, db.Close())
	raw := openRawDB(t, db.DatabasePath())
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='account_ownership_review_immutable'").Scan(&guard))
	_, err = raw.Exec("DROP TRIGGER account_ownership_review_immutable; UPDATE account_ownership_reviews SET signature=printf('%064d',0);" + guard)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	before, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.ErrorIs(t, db.Open(db.DatabasePath()), models.ErrSourcePayloadCorrupt)
	after, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestAccountReviewMigrationCollisionPreservesOriginalDatabase(t *testing.T) {
	db, _ := archiveTestDatabase(t)
	require.NoError(t, db.Close())
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	removeAccountReviewSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000067,dirty=0; CREATE TABLE account_ownership_reviews(private_data TEXT); INSERT INTO account_ownership_reviews VALUES('keep')")
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, db.Open(db.DatabasePath()), &needed)
	before, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.Error(t, db.RunAllMigrations())
	after, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}
