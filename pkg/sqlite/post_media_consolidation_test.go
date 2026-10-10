package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func consolidationMediaRequest(t *testing.T, repo models.Repository, post, media, state string, grouped bool) models.SourcePostMediaInput {
	t.Helper()
	input := models.SourcePostMediaInput{UUID: uuid.NewString(), PostUUID: post, MediaUUID: media, State: state, Origin: "review", Reason: "Reviewed consolidated post choices"}
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		p, err := repo.SourceEvidence.FindPost(ctx, post)
		require.NoError(t, err)
		m, err := repo.ArchiveEntity.Resolve(ctx, media)
		require.NoError(t, err)
		input.ExpectedPostRevision, input.ExpectedMediaRevision = p.Revision, m.Revision
		input.MediaUUID = m.UUID
		ids, err := sourceMediaAliases(ctx, m.UUID)
		require.NoError(t, err)
		var rows []sourcePostMediaRow
		if grouped {
			rows, err = consolidatedPostMediaRows(ctx, p.UUID, ids)
		} else {
			rows, err = sourcePostMediaRows(ctx, p.UUID, ids)
		}
		require.NoError(t, err)
		for _, row := range rows {
			input.ExpectedDecisions = append(input.ExpectedDecisions, row.UUID)
		}
		return nil
	}))
	return input
}

func applyConsolidationMedia(t *testing.T, repo models.Repository, input models.SourcePostMediaInput, consolidation string) *models.SourcePostMediaDecision {
	t.Helper()
	var ret *models.SourcePostMediaDecision
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		if consolidation == "" {
			ret, err = (&SourcePostMediaStore{}).decide(ctx, input, false)
		} else {
			ret, err = publishConsolidatedPostMedia(ctx, input, consolidation)
		}
		return err
	}))
	return ret
}

func TestPostMediaConsolidationKeepsOriginalHistoryMetadataAndRetryAcrossLaterMerge(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "legacy:catalog:fixture", "b")
	c := identityPost(t, repo, "native:reddit", "one-post")
	other := identityPost(t, repo, "native:reddit", "other-post")
	capture := postSelectionCapture(t, repo, a, 0, 1)
	unrelated := postSelectionCapture(t, repo, other, 0, 1)
	media := consolidationTestMedia(t, repo)
	firstRequest := consolidationMediaRequest(t, repo, a, media.UUID, "linked", false)
	first := applyConsolidationMedia(t, repo, firstRequest, "")
	for _, state := range []string{"unlinked", "linked", "unlinked"} {
		applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, a, media.UUID, state, false), "")
	}
	applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, b, media.UUID, "linked", false), "")
	evidence := identityRows(t, repo, "source_captures", "source_post_revisions", "source_attachment_manifests", "source_attachment_entries")
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	request := consolidationMediaRequest(t, repo, b, media.UUID, "linked", true)
	require.Len(t, request.ExpectedDecisions, 2)
	selected := applyConsolidationMedia(t, repo, request, merge.UUID)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var previousRevision int
		require.NoError(t, dbWrapper.Get(ctx, &previousRevision, `SELECT old.post_revision FROM post_media_consolidation_edges e
JOIN post_media_decisions old ON old.uuid=e.previous_uuid WHERE e.decision_uuid=?`, selected.UUID))
		require.Greater(t, previousRevision, selected.PostRevision, "different original post revisions are not a shared clock")
		after, err := repo.SourcePostMedia.Association(ctx, a, media.UUID)
		require.NoError(t, err)
		require.Equal(t, b, after.PostUUID)
		require.Equal(t, []models.SourcePostMediaDecision{*selected}, after.Decisions)
		originalHeads, err := sourcePostMediaRows(ctx, a, []string{media.UUID})
		require.NoError(t, err)
		require.Empty(t, originalHeads, "old current heads are retired without losing immutable history")
		require.NoError(t, repo.SourcePostMedia.ValidateCapture(ctx, selected.UUID, capture.UUID, media.UUID))
		require.ErrorIs(t, repo.SourcePostMedia.ValidateCapture(ctx, selected.UUID, unrelated.UUID, media.UUID), models.ErrSourcePostMediaConflict)
		entity, err := repo.ArchiveEntity.Find(ctx, media.UUID)
		require.NoError(t, err)
		field, err := repo.MetadataField.ApplyAutomatic(ctx, models.MetadataFieldDecisionInput{EntityUUID: media.UUID, ExpectedEntityRevision: entity.Revision,
			Field: "details", Mode: "inherit", Origin: "source", CaptureUUID: capture.UUID, Value: json.RawMessage(`"Original caption"`)})
		require.NoError(t, err)
		_, err = dbWrapper.Exec(ctx, "INSERT INTO metadata_decision_post_media(decision_uuid,post_media_decision_uuid) VALUES(?,?)", field.Decision.UUID, selected.UUID)
		return err
	}))
	secondMerge := publishPostIdentity(t, repo, identityRequest(t, repo, b, c))
	final := applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, c, media.UUID, "unlinked", true), secondMerge.UUID)
	require.Equal(t, evidence, identityRows(t, repo, "source_captures", "source_post_revisions", "source_attachment_manifests", "source_attachment_entries"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.Equal(t, first, applyConsolidationMedia(t, repo, firstRequest, ""))
	require.Equal(t, selected, applyConsolidationMedia(t, repo, request, merge.UUID))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		history, err := repo.SourcePostMedia.History(ctx, a, media.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, history, 4)
		require.Equal(t, *first, history[0])
		current, err := repo.SourcePostMedia.Association(ctx, c, media.UUID)
		require.NoError(t, err)
		require.Equal(t, "unlinked", current.State)
		require.Equal(t, []models.SourcePostMediaDecision{*final}, current.Decisions)
		for _, alias := range []string{a, b} {
			resolved, err := repo.SourcePostMedia.Association(ctx, alias, media.UUID)
			require.NoError(t, err)
			require.Equal(t, current, resolved)
		}
		require.ErrorIs(t, repo.SourcePostMedia.ValidateCapture(ctx, selected.UUID, capture.UUID, media.UUID), models.ErrSourcePostMediaConflict)
		field, err := repo.MetadataField.State(ctx, media.UUID, "details")
		require.NoError(t, err)
		require.Equal(t, selected.UUID, field.Decision.PostMediaDecisionUUID)
		require.Equal(t, capture.UUID, *field.Decision.CaptureUUID)
		return nil
	}))
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	require.NoError(t, db.Anonymise(output))
	anonymous := NewDatabase()
	require.NoError(t, anonymous.Open(output))
	defer anonymous.Close()
	rows := identityRows(t, anonymous.Repository(), "post_media_consolidation_edges", "post_media_decisions", "post_media_supersessions")
	for _, values := range rows {
		require.Empty(t, values)
	}
}

func TestPostMediaConsolidationRejectsIncompleteReviewAndMissingIdentity(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one-post")
	media := consolidationTestMedia(t, repo)
	for _, post := range []string{a, b} {
		applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, post, media.UUID, "unlinked", false), "")
	}
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	request := consolidationMediaRequest(t, repo, b, media.UUID, "linked", true)
	before := identityRows(t, repo, "source_posts", "post_media_decisions", "post_media_links", "post_media_supersessions", "post_media_consolidation_edges")
	for _, corrupt := range []string{"omit alias", "stale media", "stale post", "wrong consolidation"} {
		t.Run(corrupt, func(t *testing.T) {
			input, identity := request, merge.UUID
			switch corrupt {
			case "omit alias":
				input.ExpectedDecisions = input.ExpectedDecisions[:1]
			case "stale media":
				input.ExpectedMediaRevision++
			case "stale post":
				input.ExpectedPostRevision--
			case "wrong consolidation":
				identity = uuid.NewString()
			}
			err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := publishConsolidatedPostMedia(ctx, input, identity)
				return err
			})
			require.ErrorIs(t, err, models.ErrSourcePostMediaConflict)
		})
	}
	require.Equal(t, before, identityRows(t, repo, "source_posts", "post_media_decisions", "post_media_links", "post_media_supersessions", "post_media_consolidation_edges"))
}

func TestPostMediaConsolidationCaughtFailureRollsBackIdentityAndChoices(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one-post")
	media := consolidationTestMedia(t, repo)
	old := applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, a, media.UUID, "unlinked", false), "")
	request := identityRequest(t, repo, a, b)
	before := identityRows(t, repo, "source_posts", "source_post_identities", "source_post_consolidations", "post_media_decisions", "post_media_links", "post_media_supersessions", "post_media_consolidation_edges")
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, "CREATE TRIGGER fail_consolidated_media BEFORE INSERT ON post_media_links BEGIN SELECT RAISE(ABORT,'late media head failure'); END")
		require.NoError(t, err)
		identity, err := publishPostConsolidationIdentity(ctx, request)
		require.NoError(t, err)
		post, err := repo.SourceEvidence.FindPost(ctx, b)
		require.NoError(t, err)
		_, err = publishConsolidatedPostMedia(ctx, models.SourcePostMediaInput{UUID: uuid.NewString(), PostUUID: b, MediaUUID: media.UUID,
			ExpectedPostRevision: post.Revision, ExpectedMediaRevision: media.Revision, ExpectedDecisions: []string{old.UUID}, State: "unlinked", Origin: "review"}, identity.UUID)
		require.ErrorContains(t, err, "late media head failure")
		return nil // A caught error cannot commit the partially applied post merge.
	})
	require.ErrorIs(t, err, models.ErrSourcePostMediaConflict)
	require.Equal(t, before, identityRows(t, repo, "source_posts", "source_post_identities", "source_post_consolidations", "post_media_decisions", "post_media_links", "post_media_supersessions", "post_media_consolidation_edges"))
}

func TestPostMediaConsolidationAuditRejectsOlderUnrelatedMergeProof(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one-post")
	x := identityPost(t, repo, "legacy:catalog:fixture", "x")
	media := consolidationTestMedia(t, repo)
	applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, x, media.UUID, "linked", false), "")
	earlier := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	actual := publishPostIdentity(t, repo, identityRequest(t, repo, x, b))
	applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, b, media.UUID, "linked", true), actual.UUID)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var guard string
		require.NoError(t, dbWrapper.Get(ctx, &guard, "SELECT sql FROM sqlite_schema WHERE name='post_media_consolidation_edge_immutable'"))
		_, err := dbWrapper.Exec(ctx, "DROP TRIGGER post_media_consolidation_edge_immutable")
		require.NoError(t, err)
		_, err = dbWrapper.Exec(ctx, "UPDATE post_media_consolidation_edges SET consolidation_uuid=?", earlier.UUID)
		require.NoError(t, err)
		_, err = dbWrapper.Exec(ctx, guard)
		return err
	}))
	require.NoError(t, db.Close())
	before, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.ErrorContains(t, db.AuditForTesting(db.DatabasePath()), "invalid post media consolidation evidence")
	after, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestPostMediaConsolidationChoiceLookupStartsWithCanonicalGroupIndex(t *testing.T) {
	_, repo := postIdentityFixture(t)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var rows []struct {
			ID, Parent, Notused int
			Detail              string
		}
		require.NoError(t, dbWrapper.Select(ctx, &rows, "EXPLAIN QUERY PLAN "+consolidatedPostMediaQuery+"(?,?) LIMIT ?", "root", "one", "two", maxPostComparisonChoices+1))
		plan := fmt.Sprint(rows)
		require.Contains(t, plan, "SEARCH i USING COVERING INDEX source_post_identities_canonical (canonical_uuid=?)")
		require.Contains(t, plan, "SEARCH l USING PRIMARY KEY (post_uuid=? AND media_uuid=?)")
		require.NotContains(t, plan, "SCAN ")
		require.NotContains(t, plan, "TEMP B-TREE")
		return nil
	}))
}

func TestPostMediaConsolidationRetainsDeletedMediaWithoutResurrection(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one-post")
	capture := postSelectionCapture(t, repo, a, 0, 1)
	media := consolidationTestMedia(t, repo)
	old := applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, a, media.UUID, "linked", false), "")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Scene.Destroy(ctx, *media.LocalID) }))
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	request := consolidationMediaRequest(t, repo, b, media.UUID, "linked", true)
	selected := applyConsolidationMedia(t, repo, request, merge.UUID)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		m, err := repo.ArchiveEntity.Find(ctx, media.UUID)
		require.NoError(t, err)
		require.Equal(t, models.ArchiveEntityDeleted, m.State)
		require.Nil(t, m.LocalID)
		var scenes int
		require.NoError(t, dbWrapper.Get(ctx, &scenes, "SELECT count(*) FROM scenes"))
		require.Zero(t, scenes)
		require.ErrorIs(t, repo.SourcePostMedia.ValidateCapture(ctx, selected.UUID, capture.UUID, media.UUID), models.ErrSourcePostMediaConflict)
		original, err := repo.SourcePostMedia.Decision(ctx, old.UUID)
		require.NoError(t, err)
		require.Equal(t, old, original)
		return nil
	}))
	// Ordinary link edits still require an active library scene/image.
	ordinary := consolidationMediaRequest(t, repo, b, media.UUID, "linked", false)
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := repo.SourcePostMedia.Decide(ctx, ordinary); return err })
	require.ErrorIs(t, err, models.ErrSourcePostMediaConflict)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.Equal(t, selected, applyConsolidationMedia(t, db.Repository(), request, merge.UUID))
}

func TestPostMediaConsolidationSQLRequiresRelatedProofAndCompletedReplacement(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one-post")
	other := identityPost(t, repo, "native:reddit", "other-post")
	media := consolidationTestMedia(t, repo)
	old := applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, a, media.UUID, "linked", false), "")
	unrelated := applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, other, media.UUID, "linked", false), "")
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	input := consolidationMediaRequest(t, repo, b, media.UUID, "linked", true)
	tables := []string{"source_posts", "post_media_decisions", "post_media_links", "post_media_supersessions", "post_media_consolidation_edges"}
	before := identityRows(t, repo, tables...)
	for _, invalid := range []string{"missing proof", "unrelated original owner", "uncompleted replacement"} {
		t.Run(invalid, func(t *testing.T) {
			err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := dbWrapper.Exec(ctx, "UPDATE source_posts SET revision=revision+1 WHERE uuid=?", b)
				require.NoError(t, err)
				_, err = dbWrapper.Exec(ctx, `INSERT INTO post_media_decisions(uuid,post_uuid,media_uuid,post_revision,media_revision,state,origin,reason,request_digest)
VALUES(?,?,?,?,?,'linked','review','SQL provenance test',?)`, input.UUID, b, media.UUID, input.ExpectedPostRevision+1, media.Revision, strings.Repeat("a", 64))
				require.NoError(t, err)
				if invalid == "missing proof" {
					_, err = dbWrapper.Exec(ctx, "INSERT INTO post_media_supersessions(previous_uuid,decision_uuid) VALUES(?,?)", old.UUID, input.UUID)
					require.ErrorContains(t, err, "requires a later decision or reviewed post consolidation")
					return err
				}
				previous := old.UUID
				if invalid == "unrelated original owner" {
					previous = unrelated.UUID
				}
				_, err = dbWrapper.Exec(ctx, "INSERT INTO post_media_consolidation_edges(previous_uuid,decision_uuid,consolidation_uuid) VALUES(?,?,?)", previous, input.UUID, merge.UUID)
				if invalid == "unrelated original owner" {
					require.ErrorContains(t, err, "requires current related choices")
					return err
				}
				require.NoError(t, err)
				return nil // The deferred FK must refuse an orphan proof at commit.
			})
			require.Error(t, err)
			if invalid == "uncompleted replacement" {
				require.ErrorContains(t, err, "FOREIGN KEY")
			}
			require.Equal(t, before, identityRows(t, repo, tables...))
		})
	}
}
