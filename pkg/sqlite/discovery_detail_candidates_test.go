package sqlite_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

func detailCandidates(t *testing.T, f *detailFixture, after *models.DiscoveryDetailCursor, limit int) *models.DiscoveryDetailCandidates {
	t.Helper()
	page, err := f.worker.Candidates(t.Context(), f.tokens[0], f.listing.CollectionUUID, after, limit)
	require.NoError(t, err)
	return page
}

func TestDiscoveryDetailAutomaticAdmissionWaitsForCompleteReviewAndReplays(t *testing.T) {
	f := newDetailFixture(t)
	f.input.Automatic = true
	require.Empty(t, detailCandidates(t, f, nil, 10).Candidates)
	_, err := f.worker.Admit(t.Context(), f.tokens[0], f.input)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	finishDetailListing(t, f)
	page := detailCandidates(t, f, nil, 1)
	require.Len(t, page.Candidates, 1)
	candidate := page.Candidates[0]
	require.Equal(t, f.target.UUID, candidate.TargetUUID)
	require.Equal(t, f.input.ExpectedTargetRevision, candidate.TargetRevision)
	require.Equal(t, f.input.CandidateSequence, candidate.CandidateSequence)
	require.Equal(t, f.review(t).Candidate.URL, candidate.URL)
	require.Equal(t, candidate.Cursor, *page.After)
	require.True(t, page.HasMore)
	last := detailCandidates(t, f, page.After, 1)
	require.False(t, last.HasMore)
	require.Empty(t, last.Candidates)
	require.Equal(t, page.After, last.After)
	job := f.admit(t)
	require.Equal(t, job, f.admit(t), "lost admission response replays the same job")
	require.Empty(t, detailCandidates(t, f, nil, 10).Candidates)
	changed := f.input
	changed.PolicySHA256 = strings.Repeat("f", 64)
	_, err = f.worker.Admit(t.Context(), f.tokens[0], changed)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	finish := completeDetailFixture(t, f)
	require.Equal(t, "corroborated", finish.Evidence.Status)
	publishDetailFixture(t, f)
	require.Empty(t, detailCandidates(t, f, nil, 10).Candidates)
	ack, err := f.worker.Admit(t.Context(), f.tokens[0], f.input)
	require.NoError(t, err)
	require.Equal(t, job.UUID, ack.UUID)
	require.Equal(t, "succeeded", ack.State)
}

func TestDiscoveryDetailAutomaticAdmissionNeverResetsEarlierAttempts(t *testing.T) {
	for _, outcome := range []string{"queued", "retry", "failed", "cancelled", "negative", "positive"} {
		t.Run(outcome, func(t *testing.T) {
			f := newDetailFixture(t)
			job := f.admit(t)
			switch outcome {
			case "retry", "failed":
				running := f.claim(t, job, 0)
				code := "source_busy"
				if outcome == "failed" {
					code = "not_found"
				}
				_, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), code)
				require.NoError(t, err)
			case "cancelled":
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					_, err := f.repo.ArchiveJob.Cancel(ctx, job.UUID, job.Revision, f.now)
					return err
				}))
			case "negative", "positive":
				if outcome == "negative" {
					f.body = []byte(strings.Replace(string(f.body), "2026-10-03", "2020-01-01", 1))
				}
				result := completeDetailFixture(t, f)
				if outcome == "negative" {
					require.Equal(t, "uncorroborated", result.Evidence.Status)
				}
			}
			before, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
			require.NoError(t, err)
			finishDetailListing(t, f)
			f.input.Automatic = true
			require.Empty(t, detailCandidates(t, f, nil, 1).Candidates)
			_, err = f.worker.Admit(t.Context(), f.tokens[0], f.input)
			require.ErrorIs(t, err, models.ErrDiscoveryConflict)
			after, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
			require.NoError(t, err)
			require.Equal(t, before, after, "revision progress cannot reset an earlier deadline or outcome")
		})
	}
}

func TestDiscoveryDetailAutomaticAdmissionRechecksCurrentChoicesBeforeCommit(t *testing.T) {
	f := newDetailFixture(t)
	finishDetailListing(t, f)
	f.input.Automatic = true
	require.Len(t, detailCandidates(t, f, nil, 10).Candidates, 1)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		// Register before admission's own guard to simulate another operation in
		// the same managed transaction changing the selected source.
		txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
			collection, err := f.repo.SourceCollection.Find(ctx, f.listing.CollectionUUID)
			if err != nil {
				return err
			}
			definition := collection.SourceCollectionDefinition
			definition.State = "disabled"
			_, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID,
				ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
			return err
		})
		_, err := f.repo.DiscoveryDetail.Admit(ctx, f.input, f.now)
		return err
	})
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_detail_jobs"))
	require.Len(t, detailCandidates(t, f, nil, 10).Candidates, 1, "failed transaction restores the prior source choice")
	job := f.admit(t)
	require.NotNil(t, job)
}

func TestDiscoveryDetailAutomaticAdmissionPreservesConflictAndHistoryReview(t *testing.T) {
	for _, state := range []string{"history", "competing", "post_changed", "identifier_in_use"} {
		t.Run(state, func(t *testing.T) {
			match := newDiscoveryMatchHistoryFixture(t, state == "history")
			page := match.page
			if state == "history" {
				page = discoveryReviewFinalPage(t, match.page, match.listing.InitialCursor, false)
			}
			match.append(t, page)
			match.advance(t, 0)
			f := attachDetailFixture(t, match)
			if state == "competing" {
				f.append(t, discoveryReviewFinalPage(t, f.page, map[string]string{"after": "t3_abc123"}, true))
				f.advance(t, 1)
			} else if state != "history" {
				finishDetailListing(t, f)
			}
			f.input.Automatic = true
			f.input.ExpectedTargetRevision = f.review(t).Target.Revision
			if state == "post_changed" {
				raw := openRawDB(t, f.db.DatabasePath())
				_, err := raw.Exec("UPDATE source_posts SET revision=revision+1 WHERE uuid=?", f.target.PostUUID)
				require.NoError(t, err)
				require.NoError(t, raw.Close())
			}
			if state == "identifier_in_use" {
				sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, "")
			}
			require.Empty(t, detailCandidates(t, f, nil, 10).Candidates)
			_, err := f.worker.Admit(t.Context(), f.tokens[0], f.input)
			require.ErrorIs(t, err, models.ErrDiscoveryConflict)
		})
	}
}

func TestDiscoveryDetailAutomaticAdmissionWaitsForEarlierRecoveryComparisons(t *testing.T) {
	for _, competing := range []bool{false, true} {
		match := newDiscoveryMatchHistoryFixture(t, true)
		previousListing, previousTarget := match.listing, match.target
		match.append(t, discoveryReviewFinalPage(t, match.page, previousListing.InitialCursor, competing))
		recoverDiscoveryFixture(t, match)
		match.append(t, match.page)
		match.advance(t, 0)
		f := attachDetailFixture(t, match)
		finishDetailListing(t, f)
		f.input.Automatic = true
		require.Empty(t, detailCandidates(t, f, nil, 10).Candidates)
		_, err := f.worker.Admit(t.Context(), f.tokens[0], f.input)
		require.ErrorIs(t, err, models.ErrDiscoveryConflict)
		require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.DiscoveryMatch.Advance(ctx, previousTarget.UUID, 0, f.now)
			return err
		}))
		page := detailCandidates(t, f, nil, 10)
		if competing {
			require.Empty(t, page.Candidates)
		} else {
			require.Len(t, page.Candidates, 1)
			require.Equal(t, f.target.UUID, page.Candidates[0].TargetUUID)
		}
	}
}

func TestDiscoveryDetailInspectionBoundsEmptyListingsAndUsesScopedIndexes(t *testing.T) {
	f := newDetailFixture(t)
	finishDetailListing(t, f)
	// Definitions without targets must advance the cursor too, and cannot make
	// one candidate request scan arbitrarily many empty containers.
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for i := 1; i <= 70; i++ {
			input := f.listing.DiscoveryListingInput
			input.UUID, input.Legacy = fmt.Sprintf("00000000-0000-4000-8000-%012d", i), nil
			if _, err := f.repo.DiscoveryJob.CreateListing(ctx, input, f.now); err != nil {
				return err
			}
		}
		return nil
	}))
	var cursor *models.DiscoveryDetailCursor
	found, emptyPages, complete := 0, 0, false
	for i := 0; i < 8; i++ {
		page := detailCandidates(t, f, cursor, 1)
		found += len(page.Candidates)
		if !page.HasMore {
			complete = true
			break
		}
		require.NotNil(t, page.After)
		if len(page.Candidates) == 0 {
			emptyPages++
		}
		if cursor != nil {
			require.True(t, page.After.ListingUUID > cursor.ListingUUID ||
				(page.After.ListingUUID == cursor.ListingUUID && page.After.SourceOrdinal > cursor.SourceOrdinal))
		}
		cursor = page.After
	}
	require.True(t, complete)
	require.Equal(t, 1, found)
	require.GreaterOrEqual(t, emptyPages, 2)
	containers, err := f.worker.InspectionCollections(t.Context(), f.tokens[0], f.input.PolicySHA256, f.input.ExtractorVersion, "", 1)
	require.NoError(t, err)
	require.Equal(t, []models.EnrichmentCollectionCandidate{{UUID: f.listing.CollectionUUID}}, containers)
	containers, err = f.worker.InspectionCollections(t.Context(), f.tokens[0], f.input.PolicySHA256, f.input.ExtractorVersion, f.listing.CollectionUUID, 1)
	require.NoError(t, err)
	require.Empty(t, containers)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	for _, query := range []string{
		`SELECT d.uuid,l.snapshot_uuid FROM discovery_listings d LEFT JOIN discovery_listing_legacy l ON l.listing_uuid=d.uuid WHERE d.collection_uuid='scope' AND d.uuid>='cursor' ORDER BY d.uuid LIMIT 1`,
		`SELECT uuid,source_ordinal FROM discovery_match_targets WHERE listing_uuid='scope' AND snapshot_uuid='snapshot' AND source_ordinal>0 ORDER BY source_ordinal LIMIT 20`,
		`SELECT EXISTS(SELECT 1 FROM discovery_detail_jobs WHERE target_uuid='scope' AND candidate_sequence=1 AND job_uuid!='')`,
	} {
		rows, err := raw.Query("EXPLAIN QUERY PLAN " + query)
		require.NoError(t, err)
		defer func() { require.NoError(t, rows.Close()) }()
		var plan strings.Builder
		for rows.Next() {
			var a, b, c int
			var detail string
			require.NoError(t, rows.Scan(&a, &b, &c, &detail))
			plan.WriteString(detail)
		}
		require.NoError(t, rows.Err())
		require.Contains(t, plan.String(), "SEARCH")
		require.NotContains(t, plan.String(), "USE TEMP B-TREE")
		require.NotContains(t, plan.String(), "SCAN discovery_")
	}
	for _, after := range []*models.DiscoveryDetailCursor{{ListingUUID: "invalid"}, {ListingUUID: uuid.NewString(), SourceOrdinal: -1}} {
		_, err := f.worker.Candidates(t.Context(), f.tokens[0], f.listing.CollectionUUID, after, 1)
		require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
	}
	_, err = f.worker.Candidates(t.Context(), f.tokens[0], f.listing.CollectionUUID, nil, -1)
	require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
	f.now = f.now.Add(-time.Hour)
	require.Empty(t, detailCandidates(t, f, nil, 100).Candidates, "future observations cannot be admitted")
}
