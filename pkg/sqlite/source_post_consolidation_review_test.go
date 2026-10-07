package sqlite

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func consolidationTestMedia(t *testing.T, repo models.Repository) *models.ArchiveEntity {
	t.Helper()
	var ret *models.ArchiveEntity
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		scene := models.NewScene()
		scene.Title = "Selected media"
		if err := repo.Scene.Create(ctx, &scene, nil); err != nil {
			return err
		}
		var err error
		ret, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveScene, scene.ID)
		return err
	}))
	return ret
}

func consolidationTestMediaChoice(t *testing.T, repo models.Repository, post string, media *models.ArchiveEntity, state string) {
	t.Helper()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.SourcePostMedia.Association(ctx, post, media.UUID)
		if err != nil {
			return err
		}
		ids := []string{}
		for _, choice := range current.Decisions {
			ids = append(ids, choice.UUID)
		}
		_, err = (&SourcePostMediaStore{}).decide(ctx, models.SourcePostMediaInput{UUID: uuid.NewString(), PostUUID: post, MediaUUID: media.UUID,
			ExpectedPostRevision: current.PostRevision, ExpectedMediaRevision: current.MediaRevision, ExpectedDecisions: ids, State: state, Origin: "review"}, false)
		return err
	}))
}

func consolidationTestChoices(t *testing.T, repo models.Repository, source, destination string) *postConsolidationChoiceSnapshot {
	t.Helper()
	var ret *postConsolidationChoiceSnapshot
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = inspectPostConsolidationChoices(ctx, source, destination)
		return err
	}))
	return ret
}

func TestPostConsolidationChoicesInspectEarlierAliasesAndDetectChangedUnlink(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "legacy:catalog:fixture", "second")
	c := identityPost(t, repo, "legacy:catalog:fixture", "third")
	d := identityPost(t, repo, "native:reddit", "one-post")
	identityAlbum(t, repo, a)
	identityAlbum(t, repo, c)
	media := consolidationTestMedia(t, repo)
	consolidationTestMediaChoice(t, repo, a, media, "unlinked")
	consolidationTestMediaChoice(t, repo, c, media, "linked")
	firstMerge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	publishPostIdentity(t, repo, identityRequest(t, repo, c, d))
	before := identityRows(t, repo, "source_posts", "source_post_identities", "source_captures", "post_media_decisions", "post_gallery_decisions")
	preview := consolidationTestChoices(t, repo, b, d)
	require.Len(t, preview.Posts, 4)
	require.Equal(t, preview, consolidationTestChoices(t, repo, b, d), "review signatures and conflict ordering are stable")
	var kinds []string
	for _, conflict := range preview.Conflicts {
		kinds = append(kinds, conflict.Kind)
		if conflict.Kind == "media_choice" || conflict.Kind == "gallery_choice" {
			require.ElementsMatch(t, []string{a, c}, conflict.PostUUIDs, "conflicts expose the original choices, even when both roots have none")
		}
	}
	require.ElementsMatch(t, []string{"gallery_choice", "media_choice"}, kinds)
	require.Equal(t, before, identityRows(t, repo, "source_posts", "source_post_identities", "source_captures", "post_media_decisions", "post_gallery_decisions"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.Equal(t, preview, consolidationTestChoices(t, repo, b, d))
	applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, b, media.UUID, "linked", true), firstMerge.UUID)
	later := consolidationTestChoices(t, repo, b, d)
	require.NotEqual(t, preview.Signature, later.Signature)
	require.NotEqual(t, preview.Identity.Signature, later.Identity.Signature)
	for _, conflict := range later.Conflicts {
		require.NotEqual(t, "media_choice", conflict.Kind)
	}
}

func TestPostConsolidationChoicesIncludeSameKeyAttachmentConflictsAcrossAliases(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "legacy:catalog:fixture", "second")
	c := identityPost(t, repo, "native:reddit", "one-post")
	identityAlbum(t, repo, a)
	identityAlbum(t, repo, c)
	media := consolidationTestMedia(t, repo)
	for _, post := range []string{a, c} {
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			attachment, err := repo.SourceAttachment.Lookup(ctx, post, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "second"})
			if err != nil {
				return err
			}
			input := models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision, State: "unlinked", Origin: "review"}
			if post == c {
				input.State, input.MediaUUID, input.ExpectedMediaRevision = "linked", media.UUID, media.Revision
			}
			_, err = repo.SourceAttachment.DecideMedia(ctx, input)
			return err
		}))
	}
	publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	preview := consolidationTestChoices(t, repo, b, c)
	index := slices.IndexFunc(preview.Conflicts, func(c postConsolidationChoiceConflict) bool { return c.Kind == "attachment_choice" })
	require.GreaterOrEqual(t, index, 0)
	conflict := preview.Conflicts[index]
	require.Equal(t, "native:reddit", conflict.Namespace)
	require.Equal(t, "second", conflict.Value)
	require.ElementsMatch(t, []string{a, c}, conflict.PostUUIDs)
	// Matching qualified attachment references do not create source-list
	// disagreements merely because their original UUIDs differ.
	for _, conflict := range preview.Conflicts {
		require.NotEqual(t, "source_list_position", conflict.Kind)
	}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, "UPDATE scenes SET title=? WHERE id=?", "Changed selected media", *media.LocalID)
		return err
	}))
	changedMedia := consolidationTestChoices(t, repo, b, c)
	require.Equal(t, preview.Identity.Signature, changedMedia.Identity.Signature, "library edits have an independent revision scope")
	require.NotEqual(t, preview.Signature, changedMedia.Signature, "the encompassing review must also guard selected library records")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		attachment, err := repo.SourceAttachment.Lookup(ctx, c, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "second"})
		if err != nil {
			return err
		}
		_, err = repo.SourceAttachment.DecideMedia(ctx, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID,
			ExpectedAttachmentRevision: attachment.Revision, State: "unlinked", Origin: "review"})
		return err
	}))
	later := consolidationTestChoices(t, repo, b, c)
	require.NotEqual(t, changedMedia.Signature, later.Signature)
	for _, conflict := range later.Conflicts {
		require.NotEqual(t, "attachment_choice", conflict.Kind)
	}
}

func TestPostConsolidationChoicesRejectCombinedScopeBeyondIndividualPostLimits(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "native:reddit", "one-post")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for i, post := range []string{a, b} {
			_, err := dbWrapper.Exec(ctx, `WITH RECURSIVE ids(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<4097)
INSERT INTO source_attachments(uuid,post_uuid,namespace,value)
SELECT printf('00000000-0000-4000-8000-%012x',n+?),?,'native:reddit',CAST(n AS TEXT) FROM ids`, i*4097, post)
			if err != nil {
				return err
			}
		}
		return nil
	}))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := inspectPostConsolidationChoices(ctx, a, b)
		require.ErrorIs(t, err, models.ErrSourcePostIdentityLimit)
		return nil
	}))
}

func TestPostConsolidationChoicesManifestBudgetStartsAtSelectedCanonicalGroups(t *testing.T) {
	_, repo := postIdentityFixture(t)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var rows []struct {
			ID, Parent, Notused int
			Detail              string
		}
		require.NoError(t, dbWrapper.Select(ctx, &rows, "EXPLAIN QUERY PLAN "+postConsolidationManifestBudgetQuery, "source", "destination", 4100))
		plan := fmt.Sprint(rows)
		require.Contains(t, plan, "SEARCH i USING COVERING INDEX source_post_identities_canonical")
		require.Contains(t, plan, "SEARCH s USING PRIMARY KEY (post_uuid=?)")
		require.Contains(t, plan, "SEARCH d USING PRIMARY KEY (decision_uuid=?)")
		require.NotContains(t, plan, "SCAN ")
		require.NotContains(t, plan, "TEMP B-TREE")
		return nil
	}))
}
