package sqlite_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func (f *discoveryMatchFixture) review(t *testing.T) *models.DiscoveryMatchReview {
	t.Helper()
	var review *models.DiscoveryMatchReview
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		review, err = f.repo.DiscoveryMatch.Review(ctx, f.target.UUID)
		return err
	}))
	require.NotNil(t, review)
	return review
}

func discoveryReviewFinalPage(t *testing.T, original json.RawMessage, cursor map[string]string, competing bool) json.RawMessage {
	t.Helper()
	page, err := archive.DecodeJSONObject(original, archive.MaxDiscoveryPageBytes)
	require.NoError(t, err)
	page["cursor"], page["next_cursor"], page["complete"] = cursor, nil, true
	records := page["records"].([]any)
	records[0].(map[string]any)["patch"].(map[string]any)["date"] = "2026-10-03"
	if competing {
		page["records"] = append(records, map[string]any{"kind": "post", "base": json.Number("0"), "parent": nil, "observed_at": "2026-10-04T01:02:03.000000004Z", "patch": map[string]any{"id": "def456"}, "removed": []any{}})
	}
	body, err := archive.EncodeSourceJSON(page)
	require.NoError(t, err)
	return body
}

func TestDiscoveryMatchReviewSeparatesListingComparisonAndNativeChanges(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	initial := f.review(t)
	require.Equal(t, []string{"listing_incomplete"}, initial.Blockers)
	require.Zero(t, initial.CandidateCount)
	require.Nil(t, initial.Candidate)
	require.False(t, initial.Coverage.Complete)

	f.append(t, f.page)
	waiting := f.review(t)
	require.Equal(t, 1, waiting.Coverage.RetainedPages)
	require.Zero(t, waiting.Target.LastPage)
	require.Equal(t, []string{"listing_incomplete", "comparison_pending"}, waiting.Blockers)
	f.advance(t, 0)
	weak := f.review(t)
	require.Equal(t, []string{"listing_incomplete", "detail_required"}, weak.Blockers)
	require.Equal(t, 1, weak.CandidateCount, "album attachments share one candidate")
	require.Equal(t, 1, weak.DetailCandidateCount)
	require.True(t, weak.Candidate.NeedsDetail)

	f.append(t, discoveryReviewFinalPage(t, f.page, map[string]string{"after": "t3_abc123"}, false))
	waiting = f.review(t)
	require.True(t, waiting.Coverage.RetainedComplete)
	require.False(t, waiting.Coverage.Complete, "fetching the final page did not compare it")
	require.Contains(t, waiting.Blockers, "comparison_pending")
	f.advance(t, 1)
	strong := f.review(t)
	require.Empty(t, strong.Blockers)
	require.True(t, strong.Coverage.Complete)
	require.Equal(t, 2, strong.Coverage.RetainedPages)
	require.Equal(t, 2, strong.Target.LastPage)
	require.Equal(t, 1, strong.CandidateCount, "the same post in two pages is still one candidate")
	require.Zero(t, strong.DetailCandidateCount, "later strong evidence supersedes the weak comparison")
	require.Equal(t, "abc123", strong.Candidate.Value)
	require.Nil(t, strong.CandidatePost)

	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_post_identifiers WHERE value='abc123'"))
	beforeCaptures := queryUint(t, raw, "SELECT count(*) FROM source_captures")
	require.Equal(t, strong, f.review(t))
	require.Equal(t, beforeCaptures, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	require.Equal(t, strong, f.review(t), "inspection is derived from durable receipts after restart")

	other := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, "")
	collision := f.review(t)
	require.Equal(t, []string{"identifier_in_use"}, collision.Blockers)
	require.Equal(t, other.UUID, collision.CandidatePost.UUID)
	_, err := raw.Exec("UPDATE source_posts SET state='forgotten',revision=revision+1 WHERE uuid=?", other.UUID)
	require.NoError(t, err)
	collision = f.review(t)
	require.Equal(t, []string{"identifier_in_use"}, collision.Blockers)
	require.Equal(t, "forgotten", collision.CandidatePost.State)

	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		collection, err := f.repo.SourceCollection.Find(ctx, f.listing.CollectionUUID)
		if err != nil {
			return err
		}
		definition := collection.SourceCollectionDefinition
		definition.State = "disabled"
		if _, err := f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition}); err != nil {
			return err
		}
		_, err = f.repo.SourcePostLinks.ObserveIdentifier(ctx, models.SourcePostIdentifierInput{
			SourcePostEvidence: postLinkEvidence(f.target.PostUUID), ExpectedPostRevision: f.target.PostRevision,
			Identifier: models.SourcePostIdentifier{Namespace: "native:reddit", Value: "reviewed-elsewhere"},
		})
		return err
	}))
	changed := f.review(t)
	require.Equal(t, []string{"source_changed", "post_changed", "post_already_identified", "identifier_in_use"}, changed.Blockers)
	require.Greater(t, changed.CurrentPost.Revision, changed.Target.PostRevision)
	require.True(t, changed.Coverage.Complete, "source edits do not rewrite historical coverage")
	require.Equal(t, beforeCaptures, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
}

func TestDiscoveryMatchReviewPreservesCompetingPosts(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	f.append(t, discoveryReviewFinalPage(t, f.page, nil, true))
	f.advance(t, 0)
	review := f.review(t)
	require.True(t, review.Coverage.Complete)
	require.Equal(t, 2, review.CandidateCount)
	require.Zero(t, review.DetailCandidateCount)
	require.Nil(t, review.Candidate, "do not arbitrarily choose a candidate")
	require.Nil(t, review.CandidatePost)
	require.Equal(t, []string{"competing_candidates"}, review.Blockers)
}

func TestDiscoveryMatchReviewEmptySearchAndMissingTargets(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	f.append(t, listingContinuation(t, f.page, nil, nil, true))
	f.advance(t, 0)
	review := f.review(t)
	require.True(t, review.Coverage.Complete)
	require.Zero(t, review.CandidateCount)
	require.Nil(t, review.Candidate)
	require.Equal(t, []string{"no_candidate"}, review.Blockers)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		missing, err := f.repo.DiscoveryMatch.Review(ctx, uuid.NewString())
		require.NoError(t, err)
		require.Nil(t, missing)
		_, err = f.repo.DiscoveryMatch.Review(ctx, "bad")
		require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
		return nil
	}))
}
