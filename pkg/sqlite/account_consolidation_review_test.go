package sqlite_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func consolidationReviewPreview(t *testing.T, repo models.Repository, input models.AccountConsolidationReviewInput) *models.AccountConsolidationReviewPreview {
	t.Helper()
	var ret *models.AccountConsolidationReviewPreview
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceAccount.PreviewConsolidationReview(ctx, input)
		return err
	}))
	return ret
}

func applyConsolidationReview(repo models.Repository, input models.AccountConsolidationReviewApplyInput) (*models.AccountConsolidationReview, bool, error) {
	var ret *models.AccountConsolidationReview
	var replayed bool
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, replayed, err = repo.SourceAccount.ApplyConsolidationReview(ctx, input)
		return err
	})
	return ret, replayed, err
}

func TestAccountConsolidationReviewExactReadOnlyRecovery(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	a, b, c := createSourceAccount(t, repo, "native:reddit"), createSourceAccount(t, repo, "native:reddit"), createSourceAccount(t, repo, "native:reddit")
	p := archiveFind(t, repo, models.ArchivePerformer, 71)
	input := models.AccountConsolidationReviewInput{SourceUUID: a.UUID, DestinationUUID: b.UUID, OwnershipMode: "choose",
		Ownership: &models.AccountConsolidationChoice{State: models.AccountOwnershipLinked, PerformerUUID: p.UUID, PerformerRevision: p.Revision}, Reason: "Confirmed profile"}
	preview := consolidationReviewPreview(t, repo, input)
	require.True(t, preview.Ready)
	require.Empty(t, preview.Blockers)
	require.Equal(t, p.UUID, preview.Performer.UUID)
	require.Equal(t, 2, preview.MemberCount)
	request := models.AccountConsolidationReviewApplyInput{AccountConsolidationReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		missing, err := repo.SourceAccount.ConsolidationReview(ctx, request)
		require.Nil(t, missing)
		return err
	}))
	require.Equal(t, a, findSourceAccount(t, repo, a.UUID), "receipt lookup must not perform the consolidation")
	// Independently encode the pre-existing domain contract. A tag or field
	// change must not quietly invalidate requests retained before this API.
	legacy := models.AccountConsolidationInput{SourceUUID: a.UUID, DestinationUUID: b.UUID, Signature: request.Digest, OwnershipMode: "choose",
		Ownership: models.AccountOwnershipSelection{State: input.Ownership.State, PerformerUUID: p.UUID, PerformerRevision: p.Revision}, Origin: "review", Reason: input.Reason}
	rawInput, err := json.Marshal(legacy)
	require.NoError(t, err)
	digest := sha256.Sum256(rawInput)
	receipt, replayed, err := applyConsolidationReview(repo, request)
	require.NoError(t, err)
	require.False(t, replayed)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	var stored string
	require.NoError(t, raw.QueryRow("SELECT request_digest FROM source_account_consolidations WHERE uuid=?", request.RequestUUID).Scan(&stored))
	require.Equal(t, hex.EncodeToString(digest[:]), stored)
	require.Equal(t, request, receipt.Request)
	require.Equal(t, b.UUID, receipt.Consolidation.DestinationUUID)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, p.UUID, uuid.NewString(), p.Revision)
		return err
	}))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Merge(ctx, []int{71}, 72) }))
	decideAccountOwner(t, repo, b.UUID, models.AccountOwnershipUnlinked, nil)
	_, err = consolidateAccount(repo, consolidationInput(accountPreview(t, repo, b.UUID, c.UUID)))
	require.NoError(t, err)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Destroy(ctx, 72) }))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	before := findSourceAccount(t, repo, c.UUID)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		read, err := repo.SourceAccount.ConsolidationReview(ctx, request)
		require.Equal(t, receipt, read)
		return err
	}))
	again, replayed, err := applyConsolidationReview(repo, request)
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, receipt, again)
	require.Equal(t, before, findSourceAccount(t, repo, c.UUID), "old receipt cannot restore a superseded owner or redirect")
	for _, mutate := range []func(*models.AccountConsolidationReviewApplyInput){
		func(r *models.AccountConsolidationReviewApplyInput) { r.Reason = "different" },
		func(r *models.AccountConsolidationReviewApplyInput) { r.AcceptIdentifierConflicts = true },
		func(r *models.AccountConsolidationReviewApplyInput) { r.DestinationUUID = c.UUID },
		func(r *models.AccountConsolidationReviewApplyInput) { r.Digest = strings.Repeat("0", 64) },
		func(r *models.AccountConsolidationReviewApplyInput) {
			r.Ownership = &models.AccountConsolidationChoice{State: models.AccountOwnershipUndecided}
		},
	} {
		changed := request
		mutate(&changed)
		require.ErrorIs(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceAccount.ConsolidationReview(ctx, changed)
			return err
		}), models.ErrAccountConsolidationReplay)
		_, _, err := applyConsolidationReview(repo, changed)
		require.ErrorIs(t, err, models.ErrAccountConsolidationReplay)
	}
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM source_account_consolidations"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestAccountConsolidationReviewBlockersAndStaleChoice(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	a, b := createSourceAccount(t, repo, "native:instagram"), createSourceAccount(t, repo, "native:instagram")
	for i, account := range []*models.SourceAccount{a, b} {
		observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "id", Value: []string{"111", "222"}[i]}, accountEvidence())
	}
	p := archiveFind(t, repo, models.ArchivePerformer, 71)
	decideAccountOwner(t, repo, a.UUID, models.AccountOwnershipLinked, p)
	decideAccountOwner(t, repo, b.UUID, models.AccountOwnershipUnlinked, nil)
	input := models.AccountConsolidationReviewInput{SourceUUID: a.UUID, DestinationUUID: b.UUID, OwnershipMode: "preserve"}
	preview := consolidationReviewPreview(t, repo, input)
	require.False(t, preview.Ready)
	require.ElementsMatch(t, []string{"identifiers", "ownership"}, preview.Blockers)
	require.Equal(t, []string{"111", "222"}, preview.IdentifierConflicts[0].Values)
	require.Nil(t, preview.Ownership)
	request := models.AccountConsolidationReviewApplyInput{AccountConsolidationReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
	_, _, err := applyConsolidationReview(repo, request)
	require.ErrorIs(t, err, models.ErrAccountIdentifierResolution)
	request.AcceptIdentifierConflicts = true
	_, _, err = applyConsolidationReview(repo, request)
	require.ErrorIs(t, err, models.ErrAccountOwnershipResolution)
	request.OwnershipMode = "choose"
	chosen := archiveFind(t, repo, models.ArchivePerformer, 72)
	request.Ownership = &models.AccountConsolidationChoice{State: models.AccountOwnershipLinked, PerformerUUID: chosen.UUID, PerformerRevision: chosen.Revision}
	preview = consolidationReviewPreview(t, repo, request.AccountConsolidationReviewInput)
	require.True(t, preview.Ready)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, chosen.UUID, uuid.NewString(), chosen.Revision)
		return err
	}))
	_, _, err = applyConsolidationReview(repo, request)
	require.ErrorIs(t, err, models.ErrSourceAccountConflict, "selected third performer has an independent revision fence")
	require.Nil(t, findSourceAccount(t, repo, a.UUID).RedirectTo)
	request.Ownership = &models.AccountConsolidationChoice{State: models.AccountOwnershipUnlinked}
	preview = consolidationReviewPreview(t, repo, request.AccountConsolidationReviewInput)
	request.Digest = preview.Digest
	observeAccount(t, repo, b.UUID, models.AccountReference{Namespace: b.Namespace, Kind: "handle", Value: "newhandle"}, accountEvidence())
	_, _, err = applyConsolidationReview(repo, request)
	require.ErrorIs(t, err, models.ErrSourceAccountConflict)
	request.Digest = consolidationReviewPreview(t, repo, request.AccountConsolidationReviewInput).Digest
	_, _, err = applyConsolidationReview(repo, request)
	require.NoError(t, err)
	for _, namespace := range []string{"native:twitter", "mirror:coomer:instagram"} {
		other := createSourceAccount(t, repo, namespace)
		input.SourceUUID, input.DestinationUUID = b.UUID, other.UUID
		require.ErrorIs(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceAccount.PreviewConsolidationReview(ctx, input)
			return err
		}), models.ErrAccountConsolidationReviewInvalid)
	}
}

func TestAccountConsolidationReviewRollbackAndConcurrentRecovery(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	a, b := createSourceAccount(t, repo, "native:bluesky"), createSourceAccount(t, repo, "native:bluesky")
	input := models.AccountConsolidationReviewInput{SourceUUID: a.UUID, DestinationUUID: b.UUID, OwnershipMode: "preserve"}
	request := models.AccountConsolidationReviewApplyInput{AccountConsolidationReviewInput: input, RequestUUID: uuid.NewString(), Digest: consolidationReviewPreview(t, repo, input).Digest}
	require.Error(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := repo.SourceAccount.ApplyConsolidationReview(ctx, request)
		return err
	}))
	rollback := errors.New("rollback after completed operation")
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := repo.SourceAccount.ApplyConsolidationReview(ctx, request)
		require.NoError(t, err)
		return rollback
	}), rollback)
	require.Equal(t, a, findSourceAccount(t, repo, a.UUID))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		missing, err := repo.SourceAccount.ConsolidationReview(ctx, request)
		require.Nil(t, missing)
		return err
	}))
	type result struct {
		receipt  *models.AccountConsolidationReview
		replayed bool
		err      error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			receipt, replayed, err := applyConsolidationReview(repo, request)
			results <- result{receipt, replayed, err}
		}()
	}
	one, two := <-results, <-results
	require.NoError(t, one.err)
	require.NoError(t, two.err)
	require.NotEqual(t, one.replayed, two.replayed)
	require.Equal(t, one.receipt, two.receipt)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_account_consolidations"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM account_performer_decisions"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM performers_scenes WHERE scene_id=31 AND performer_id=71"))
}
