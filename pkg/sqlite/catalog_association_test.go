package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func catalogPostClaim(t *testing.T, repo models.Repository, post, account string) {
	t.Helper()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourcePostLinks.ClaimAccount(ctx, models.SourcePostAccountClaimInput{
			SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post, Origin: "migration", Basis: "catalog-posts", ObservedAt: time.Now().UTC(), Details: []byte(`{}`)}, AccountUUID: account,
		})
		return err
	}))
}

func catalogPostFolder(t *testing.T, repo models.Repository, post, folder string) {
	t.Helper()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		collection, err := repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: uuid.NewString(), Origin: "migration",
			SourceCollectionDefinition: models.SourceCollectionDefinition{Kind: "directory", Label: folder, State: "disabled"}})
		if err != nil {
			return err
		}
		_, err = repo.SourceCollection.RecordPostMembership(ctx, models.CollectionPostMembership{
			SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post, Origin: "migration", Basis: "catalog-membership", ObservedAt: time.Now().UTC(), Details: []byte(`{}`)},
			CollectionUUID:     collection.UUID, CollectionRevision: collection.Revision,
		})
		return err
	}))
}

func catalogAssociationBackfill(t *testing.T, repo models.Repository, post string, owners bool) *models.CatalogAssociationResult {
	t.Helper()
	var result *models.CatalogAssociationResult
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = repo.CapturePublisher.BackfillCatalogAssociation(ctx, post, owners)
		return err
	}))
	return result
}

func TestCatalogAssociationRepairsPublisherAndUniqueAliasWithoutTaggingMedia(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	capture := publisherCapture(t, repo, "native:reddit", "reddit", `{"category":"reddit","title":"Legacy title"}`)
	account := createSourceAccount(t, repo, "native:reddit")
	observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "handle", Value: "cutelilasya"}, accountEvidence())
	attachmentSQL(t, db, "INSERT INTO performer_names(performer_id,name,position) VALUES(71,'CuteLilAsya',1)")
	catalogPostClaim(t, repo, capture.PostUUID, account.UUID)
	catalogPostFolder(t, repo, capture.PostUUID, "CuteLilAsya, reddit")
	first := catalogAssociationBackfill(t, repo, capture.PostUUID, true)
	require.Equal(t, "linked", first.Action)
	require.Equal(t, "linked", first.Ownership)
	require.Equal(t, archiveFind(t, repo, models.ArchivePerformer, 71).UUID, first.PerformerUUID)
	require.Contains(t, publisherPreview(t, repo, capture.UUID, "").Current.Reason, "catalog-association-v1")
	require.Equal(t, "migration", publisherPreview(t, repo, capture.UUID, "").Current.Origin)
	require.Equal(t, "already_linked", catalogAssociationBackfill(t, repo, capture.PostUUID, true).Reason)
	raw := openRawDB(t, db.DatabasePath())
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM capture_publisher_decisions"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM account_performer_decisions"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM performers_scenes"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM performers_images"))
	require.NoError(t, raw.Close())
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Merge(ctx, []int{71}, 72) }))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.Equal(t, "already_linked", catalogAssociationBackfill(t, repo, capture.PostUUID, true).Reason)
	require.Equal(t, 72, *accountReviewState(t, repo, account.UUID).Ownership.Performer.LocalID)
	_, err := applyPublisher(repo, publisherInput(publisherPreview(t, repo, capture.UUID, ""), "unlink"))
	require.NoError(t, err)
	require.Equal(t, "existing_publisher_choice", catalogAssociationBackfill(t, repo, capture.PostUUID, true).Reason)
}

func TestCatalogAssociationRejectsConflictingAuthorAndAccounts(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:reddit")
	observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "handle", Value: "expected"}, accountEvidence())
	observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "id", Value: "t2_expected"}, accountEvidence())
	for _, payload := range []string{
		`{"category":"reddit","author":"other"}`,
		`{"category":"reddit","author":"expected","author_fullname":"t2_other"}`,
	} {
		capture := publisherCapture(t, repo, "native:reddit", "reddit", payload)
		catalogPostClaim(t, repo, capture.PostUUID, account.UUID)
		require.Equal(t, "captured_author_conflict", catalogAssociationBackfill(t, repo, capture.PostUUID, true).Reason)
	}
	capture := publisherCapture(t, repo, "native:reddit", "reddit", `{"category":"reddit"}`)
	catalogPostClaim(t, repo, capture.PostUUID, account.UUID)
	other := createSourceAccount(t, repo, "native:reddit")
	catalogPostClaim(t, repo, capture.PostUUID, other.UUID)
	require.Equal(t, "conflicting_imported_accounts", catalogAssociationBackfill(t, repo, capture.PostUUID, true).Reason)
}

func TestCatalogAssociationUsesFolderIDsAndRejectsNameAmbiguity(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, "UPDATE performer_names SET name='Publisher' WHERE performer_id=71 AND position=0; INSERT INTO performer_names(performer_id,name,position) VALUES(72,'publisher',1)")
	account := createSourceAccount(t, repo, "native:twitter")
	observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "handle", Value: "publisher"}, accountEvidence())
	observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "id", Value: "123"}, accountEvidence())
	capture := publisherCapture(t, repo, "native:twitter", "twitter", `{"category":"twitter"}`)
	catalogPostFolder(t, repo, capture.PostUUID, "Publisher, 123, twitter")
	result := catalogAssociationBackfill(t, repo, capture.PostUUID, true)
	require.Equal(t, "linked", result.Action)
	require.Equal(t, "ambiguous_performers", result.Ownership)
	require.Len(t, result.PerformerCandidates, 2)
	wrong := publisherCapture(t, repo, "native:twitter", "twitter", `{"category":"twitter"}`)
	catalogPostFolder(t, repo, wrong.PostUUID, "Publisher, 456, twitter")
	require.Equal(t, "folder_account_conflict", catalogAssociationBackfill(t, repo, wrong.PostUUID, false).Reason)
	saved := publisherCapture(t, repo, "native:reddit", "reddit", `{"category":"reddit"}`)
	catalogPostFolder(t, repo, saved.PostUUID, "publisher saved, reddit")
	require.Equal(t, "no_imported_account", catalogAssociationBackfill(t, repo, saved.PostUUID, true).Reason)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		account, err = repo.SourceAccount.Create(ctx, "native:reddit", "publisher saved")
		return err
	}))
	catalogPostClaim(t, repo, saved.PostUUID, account.UUID)
	require.Equal(t, "non_author_directory", catalogAssociationBackfill(t, repo, saved.PostUUID, true).Reason)
}

func TestCatalogAssociationPreservesOwnerUnlinkAndAnyCaptureChoice(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	capture := publisherCapture(t, repo, "native:reddit", "reddit", `{"category":"reddit"}`)
	account := createSourceAccount(t, repo, "native:reddit")
	catalogPostClaim(t, repo, capture.PostUUID, account.UUID)
	_, _, err := applyAccountReview(repo, accountReviewRequest(t, repo, accountReviewInput(t, repo, account.UUID, 0)))
	require.NoError(t, err)
	result := catalogAssociationBackfill(t, repo, capture.PostUUID, true)
	require.Equal(t, "linked", result.Action)
	require.Equal(t, "preserved", result.Ownership)
	require.Equal(t, models.AccountOwnershipUnlinked, accountReviewState(t, repo, account.UUID).Ownership.State)
	second := recordSourceTestCapture(t, repo, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: capture.PostUUID,
		Origin: "gallery-dl", Platform: "reddit", CapturedAt: time.Now().UTC(), RetentionPolicy: capture.RetentionPolicy, Payload: *capture.Payload})
	_, err = applyPublisher(repo, publisherInput(publisherPreview(t, repo, second.UUID, ""), "unlink"))
	require.NoError(t, err)
	require.Equal(t, "existing_publisher_choice", catalogAssociationBackfill(t, repo, capture.PostUUID, true).Reason)
}

func TestCatalogAssociationMirrorFoldersStayWithinTheirService(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	var account *models.SourceAccount
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		account, err = repo.SourceAccount.Create(ctx, "mirror:coomer:onlyfans", "yumae")
		if err == nil {
			_, err = repo.SourceAccount.Create(ctx, "mirror:kemono:patreon", "yumae")
		}
		return err
	}))
	capture := publisherCapture(t, repo, "mirror:coomer:onlyfans", "coomer", `{"category":"coomer","service":"onlyfans"}`)
	catalogPostFolder(t, repo, capture.PostUUID, "yumae, onlyfans")
	result := catalogAssociationBackfill(t, repo, capture.PostUUID, false)
	require.Equal(t, "linked", result.Action)
	require.Equal(t, account.UUID, result.AccountUUID)
}

func TestCatalogAssociationRollsBackPublisherIfOwnershipFails(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	capture := publisherCapture(t, repo, "native:reddit", "reddit", `{"category":"reddit"}`)
	account := createSourceAccount(t, repo, "native:reddit")
	observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "handle", Value: "publisher"}, accountEvidence())
	attachmentSQL(t, db, `INSERT INTO performer_names(performer_id,name,position) VALUES(71,'Publisher',1);
 CREATE TRIGGER fail_catalog_owner BEFORE INSERT ON account_performer_decisions
 WHEN NEW.origin='migration' BEGIN SELECT RAISE(ABORT,'fixture unavailable'); END`)
	catalogPostClaim(t, repo, capture.PostUUID, account.UUID)
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.CapturePublisher.BackfillCatalogAssociation(ctx, capture.PostUUID, true)
		require.ErrorContains(t, err, "fixture unavailable")
		return nil // Even a caller swallowing the error cannot commit half a repair.
	})
	require.ErrorContains(t, err, "did not finish atomically")
	require.Nil(t, publisherPreview(t, repo, capture.UUID, "").Current)
}
