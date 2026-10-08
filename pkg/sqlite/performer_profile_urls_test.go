package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removePerformerProfileURLSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	var exists bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM native_migration_history WHERE version=1000097)").Scan(&exists))
	if exists {
		_, err := raw.Exec("DROP TABLE account_profile_urls; DROP TABLE performer_profile_url_suppressions; DELETE FROM native_migration_history WHERE version=1000097")
		require.NoError(t, err)
	}
}

func profileCapture(t *testing.T, repo models.Repository, bio string) *models.CapturePublisherDecision {
	t.Helper()
	capture := publisherCapture(t, repo, "native:twitter", "twitter", fmt.Sprintf(`{"category":"twitter","author":{"id":123,"name":"Example","description":%q}}`, bio))
	preview := publisherPreview(t, repo, capture.UUID, "")
	decision, err := applyPublisher(repo, publisherInput(preview, "automatic"))
	require.NoError(t, err)
	return decision
}

func profileOwner(t *testing.T, repo models.Repository, account string, performerID int) {
	t.Helper()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		a, err := repo.SourceAccount.Find(ctx, account)
		require.NoError(t, err)
		p, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, performerID)
		require.NoError(t, err)
		_, err = repo.SourceAccount.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: a.UUID, ExpectedAccountRevision: a.Revision,
			State: models.AccountOwnershipLinked, PerformerUUID: p.UUID, ExpectedPerformerRevision: p.Revision, Origin: "review"})
		return err
	}))
}

func performerProfileURLs(t *testing.T, repo models.Repository, id int) []string {
	t.Helper()
	var urls []string
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		urls, err = repo.Performer.GetURLs(ctx, id)
		return err
	}))
	return urls
}

func editProfileURLs(t *testing.T, repo models.Repository, id int, mode models.RelationshipUpdateMode, urls ...string) {
	t.Helper()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		update := models.NewPerformerPartial()
		update.URLs = &models.UpdateStrings{Values: urls, Mode: mode}
		_, err := repo.Performer.UpdatePartial(ctx, id, update)
		return err
	}))
}

func TestPerformerProfileURLsAddAfterOwnershipAndRespectPruning(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	first := profileCapture(t, repo, "https://site.invalid https://x.com/example")
	require.Empty(t, performerProfileURLs(t, repo, 71), "no ownership by name or depicted performer")
	editProfileURLs(t, repo, 71, models.RelationshipUpdateModeSet, "https://twitter.com/Example/", "https://manual.invalid")
	profileOwner(t, repo, *first.AccountUUID, 71)
	require.Equal(t, []string{"https://twitter.com/Example/", "https://manual.invalid", "https://site.invalid"}, performerProfileURLs(t, repo, 71))
	require.Empty(t, performerProfileURLs(t, repo, 72))
	// Cosmetic changes are not removals; an explicit removal of a preexisting
	// manual URL is remembered even if it was never automatically inserted.
	editProfileURLs(t, repo, 71, models.RelationshipUpdateModeSet, "https://site.invalid/", "https://manual.invalid")
	editProfileURLs(t, repo, 71, models.RelationshipUpdateModeRemove, "https://site.invalid/")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	profileCapture(t, repo, "https://site.invalid https://x.com/example https://new.invalid")
	require.Equal(t, []string{"https://manual.invalid", "https://new.invalid"}, performerProfileURLs(t, repo, 71))
	// Explicit manual restoration is allowed; pruning again still persists.
	editProfileURLs(t, repo, 71, models.RelationshipUpdateModeAdd, "https://site.invalid")
	profileCapture(t, repo, "https://site.invalid")
	require.Contains(t, performerProfileURLs(t, repo, 71), "https://site.invalid")
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM account_profile_urls"), "account links are shared across captures")
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM performer_profile_url_suppressions"))
}

func TestPerformerProfileURLFullUpdatesRollbackAndMerge(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	first := profileCapture(t, repo, "https://removed.invalid https://kept.invalid")
	profileOwner(t, repo, *first.AccountUUID, 71)
	require.Error(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		p, err := repo.Performer.Find(ctx, 71)
		require.NoError(t, err)
		p.URLs = models.NewRelatedStrings([]string{})
		require.NoError(t, repo.Performer.Update(ctx, &models.UpdatePerformerInput{Performer: p}))
		return errors.New("later operation failed")
	}))
	require.Len(t, performerProfileURLs(t, repo, 71), 2)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		p, err := repo.Performer.Find(ctx, 71)
		require.NoError(t, err)
		p.URLs = models.NewRelatedStrings([]string{"https://kept.invalid"})
		return repo.Performer.Update(ctx, &models.UpdatePerformerInput{Performer: p})
	}))
	// UUID adoption and merge must preserve both explicit removals and URLs
	// excluded by the merge's final destination choice.
	p := archiveFind(t, repo, models.ArchivePerformer, 71)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, p.UUID, uuid.NewString(), p.Revision)
		return err
	}))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Merge(ctx, []int{71}, 72) }))
	profileCapture(t, repo, "https://removed.invalid https://kept.invalid https://new.invalid")
	require.Equal(t, []string{"https://new.invalid"}, performerProfileURLs(t, repo, 72))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	// Deleting the performer does not recreate them or reuse their local ID.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Destroy(ctx, 72) }))
	profileCapture(t, repo, "https://another.invalid")
}

func TestPerformerProfileURLsDoNotUseUnlinkedPublishersOrOwners(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	first := profileCapture(t, repo, "https://site.invalid")
	preview := publisherPreview(t, repo, first.CaptureUUID, "")
	_, err := applyPublisher(repo, publisherInput(preview, "unlink"))
	require.NoError(t, err)
	profileOwner(t, repo, *first.AccountUUID, 71)
	require.Empty(t, performerProfileURLs(t, repo, 71))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		a, err := repo.SourceAccount.Find(ctx, *first.AccountUUID)
		require.NoError(t, err)
		_, err = repo.SourceAccount.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: a.UUID, ExpectedAccountRevision: a.Revision, State: models.AccountOwnershipUnlinked, Origin: "review"})
		return err
	}))
	profileCapture(t, repo, "https://site.invalid")
	require.Empty(t, performerProfileURLs(t, repo, 71))
}

func TestPerformerProfileURLPublicationRollsBackWithCaptureChoice(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	first := profileCapture(t, repo, "https://site.invalid")
	profileOwner(t, repo, *first.AccountUUID, 71)
	capture := publisherCapture(t, repo, "native:twitter", "twitter", `{"category":"twitter","author":{"id":123,"name":"Example","url":"https://uncommitted.invalid"}}`)
	request := publisherInput(publisherPreview(t, repo, capture.UUID, ""), "automatic")
	require.Error(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.CapturePublisher.Apply(ctx, request)
		require.NoError(t, err)
		return errors.New("capture receipt failed")
	}))
	require.Equal(t, []string{"https://site.invalid"}, performerProfileURLs(t, repo, 71))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM account_profile_urls"))
	_, err := applyPublisher(repo, request)
	require.NoError(t, err)
	require.Equal(t, []string{"https://site.invalid", "https://uncommitted.invalid"}, performerProfileURLs(t, repo, 71))
}

func TestPerformerProfileURLsFollowAccountConsolidation(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	first := profileCapture(t, repo, "https://site.invalid https://pruned.invalid")
	destination := createSourceAccount(t, repo, "native:twitter")
	profileOwner(t, repo, destination.UUID, 71)
	editProfileURLs(t, repo, 71, models.RelationshipUpdateModeAdd, "https://pruned.invalid")
	editProfileURLs(t, repo, 71, models.RelationshipUpdateModeRemove, "https://pruned.invalid")
	preview := accountPreview(t, repo, *first.AccountUUID, destination.UUID)
	input := consolidationInput(preview)
	_, err := consolidateAccount(repo, input)
	require.NoError(t, err)
	require.Equal(t, []string{"https://site.invalid"}, performerProfileURLs(t, repo, 71))
	_, err = consolidateAccount(repo, input)
	require.NoError(t, err, "the original signed consolidation remains replayable")
	profileCapture(t, repo, "https://site.invalid https://pruned.invalid https://new.invalid")
	require.ElementsMatch(t, []string{"https://site.invalid", "https://new.invalid"}, performerProfileURLs(t, repo, 71))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestPerformerProfileURLMigrationAndAnonymisation(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint("migration collision ", collision), func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			editProfileURLs(t, repo, 71, models.RelationshipUpdateModeAdd, "https://existing.invalid")
			require.NoError(t, db.Close())
			raw := openRawDB(t, db.DatabasePath())
			defer raw.Close()
			before := albumJobRows(t, raw, "archive_entities")
			removePerformerProfileURLSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000096,dirty=0")
			require.NoError(t, err)
			if collision {
				_, err = raw.Exec("CREATE TABLE account_profile_urls(private_data TEXT); INSERT INTO account_profile_urls VALUES('kept')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.ErrorAs(t, db.Open(db.DatabasePath()), &needed)
			err = db.RunAllMigrations()
			if collision {
				require.ErrorContains(t, err, "destination account_profile_urls already exists")
				var kept string
				require.NoError(t, raw.QueryRow("SELECT private_data FROM account_profile_urls").Scan(&kept))
				require.Equal(t, "kept", kept)
			} else {
				require.NoError(t, err)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM performer_profile_url_suppressions"), "absence before this feature cannot imply removal")
			}
			require.Equal(t, before, albumJobRows(t, raw, "archive_entities"))
			require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM performer_urls"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
	db, repo := archiveTestDatabase(t)
	first := profileCapture(t, repo, "https://private.invalid")
	profileOwner(t, repo, *first.AccountUUID, 71)
	editProfileURLs(t, repo, 71, models.RelationshipUpdateModeSet)
	destination := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(db, destination)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	raw := openRawDB(t, destination)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM account_profile_urls"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM performer_profile_url_suppressions"))
}
