package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

func completeDetailFixture(t *testing.T, f *detailFixture) *models.DiscoveryDetailResult {
	t.Helper()
	running := f.claim(t, f.admit(t), 0)
	checkpoint, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.body)
	require.NoError(t, err)
	result, err := f.worker.Complete(t.Context(), f.tokens[0], running.Lease(), checkpoint.Revision, checkpoint.Digest)
	require.NoError(t, err)
	return result
}

func finishDetailListing(t *testing.T, f *detailFixture) {
	t.Helper()
	f.append(t, listingContinuation(t, f.page, map[string]string{"after": "t3_abc123"}, nil, true))
	f.advance(t, 1)
	f.input.ExpectedTargetRevision = f.review(t).Target.Revision
}

func detailPublicationInput(t *testing.T, f *detailFixture) models.DiscoveryPublicationInput {
	t.Helper()
	review := f.review(t)
	require.NotNil(t, review.Detail)
	return models.DiscoveryPublicationInput{TargetUUID: review.Target.UUID, ExpectedTargetRevision: review.Target.Revision, DetailJobUUID: review.Detail.JobUUID}
}

func publishDetailFixture(t *testing.T, f *detailFixture) *models.DiscoveryMatchPublication {
	t.Helper()
	worker := ingest.NewDiscoveryPublicationWorker(f.service)
	worker.Now = func() time.Time { return f.now }
	progress, err := worker.Process(t.Context())
	require.NoError(t, err)
	require.NotNil(t, progress.Publication)
	return progress.Publication
}

func TestDiscoveryDetailPublicationKeepsOriginalObservationsAcrossFailoverAndListingProgress(t *testing.T) {
	f := newDetailFixture(t)
	candidate := *f.review(t).Candidate
	job := f.admit(t)
	first := f.claim(t, job, 0)
	partial := detailPartial(t, f.body)
	checkpoint1, err := f.worker.Checkpoint(t.Context(), f.tokens[0], first.Lease(), 0, partial)
	require.NoError(t, err)
	f.now = f.now.Add(2 * time.Minute)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryDetail.Maintain(ctx, f.now)
		return err
	}))
	f.now = f.now.Add(5 * time.Minute)
	second := f.claim(t, job, 1)
	require.NotNil(t, second)
	value, err := archive.DecodeJSONObject(f.body, archive.MaxEnrichmentTranscriptBytes)
	require.NoError(t, err)
	value["unresolved"] = []any{map[string]any{"url": "https://imgur.com/child", "parent": 0, "depth": 1, "reason": "unsupported_extractor"}}
	f.body, err = archive.EncodeSourceJSON(value)
	require.NoError(t, err)
	checkpoint2, err := f.worker.Checkpoint(t.Context(), f.tokens[1], second.Lease(), checkpoint1.Revision, f.body)
	require.NoError(t, err)
	result, err := f.worker.Complete(t.Context(), f.tokens[1], second.Lease(), checkpoint2.Revision, checkpoint2.Digest)
	require.NoError(t, err)
	require.Equal(t, "corroborated", result.Evidence.Status)
	// Additional listing progress keeps the same original candidate. No second
	// detail request is necessary solely because the target revision advanced.
	finishDetailListing(t, f)
	review := f.review(t)
	require.Greater(t, review.Target.Revision, 2)
	require.Equal(t, candidate, *review.Candidate)
	require.True(t, review.Candidate.NeedsDetail, "the original weak evidence is immutable")
	require.Empty(t, review.Blockers)
	input := detailPublicationInput(t, f)
	publication := publishDetailFixture(t, f)
	require.Equal(t, archive.DiscoveryDetailPublicationPolicy, publication.Policy)
	require.Equal(t, job.UUID, *publication.DetailJobUUID)
	require.Equal(t, 3, publication.RecordCount)
	require.Equal(t, "exact-title-and-date", publication.Basis)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		originals, err := f.repo.DiscoveryDetail.CheckpointRecords(ctx, job.UUID, -1, 100)
		require.NoError(t, err)
		require.Equal(t, f.producers[0], originals[0].ProducerUUID)
		require.Equal(t, f.producers[1], originals[1].ProducerUUID)
		work, err := archive.DecodeDiscoveryDetailJob(job)
		require.NoError(t, err)
		expected, err := archive.PrepareDiscoveryDetailCaptures(job.UUID, f.target.PostUUID, work.PostIdentifier(), work.URL, work.ExtractorVersion, f.body, originals)
		require.NoError(t, err)
		published, err := f.repo.DiscoveryMatch.PublishedRecords(ctx, f.target.UUID, -1, 100)
		require.NoError(t, err)
		require.Len(t, published, len(expected))
		for i, record := range published {
			require.Equal(t, expected[i].Input.UUID, record.CaptureUUID)
			capture, err := f.repo.SourceEvidence.FindCapture(ctx, record.CaptureUUID)
			require.NoError(t, err)
			require.True(t, expected[i].Input.CapturedAt.Equal(capture.CapturedAt))
		}
		return nil
	}))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM discovery_detail_checkpoints"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM discovery_pages"))
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM source_collection_captures WHERE capture_uuid IN (SELECT capture_uuid FROM discovery_published_records)"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten',revision=revision+1 WHERE uuid=?", f.target.PostUUID)
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	f.service.Repo = f.repo
	replayed, err := f.service.PublishDiscoveryMatch(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, publication, replayed)
	comparison, err := f.worker.Complete(t.Context(), f.tokens[1], second.Lease(), checkpoint2.Revision, checkpoint2.Digest)
	require.NoError(t, err)
	require.Equal(t, result, comparison)
	ack, err := f.worker.Checkpoint(t.Context(), f.tokens[0], first.Lease(), 0, partial)
	require.NoError(t, err)
	require.Equal(t, checkpoint1, ack)
	input.DetailJobUUID = uuid.NewString()
	_, err = f.service.PublishDiscoveryMatch(t.Context(), input)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict, "publication replay retains the selected proof")
}

func TestDiscoveryDetailPublicationCannotBypassReview(t *testing.T) {
	for _, state := range []string{"unfinished", "competing", "post_changed", "source_changed", "identifier_in_use", "negative", "preview_only"} {
		t.Run(state, func(t *testing.T) {
			f := newDetailFixture(t)
			if state == "negative" {
				body, err := archive.DecodeJSONObject(f.body, archive.MaxEnrichmentTranscriptBytes)
				require.NoError(t, err)
				body["records"].([]any)[0].(map[string]any)["patch"].(map[string]any)["date"] = "2020-01-01"
				f.body, err = archive.EncodeSourceJSON(body)
				require.NoError(t, err)
			}
			var detail string
			if state != "preview_only" {
				detail = completeDetailFixture(t, f).JobUUID
			}
			if state == "competing" {
				f.append(t, discoveryReviewFinalPage(t, f.page, map[string]string{"after": "t3_abc123"}, true))
				f.advance(t, 1)
			} else if state != "unfinished" {
				finishDetailListing(t, f)
			}
			if state == "preview_only" {
				review := f.review(t)
				require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
					preview, err := f.repo.DiscoveryMatch.PreviewDetail(ctx, models.DiscoveryDetailPreviewInput{TargetUUID: f.target.UUID,
						ExpectedTargetRevision: review.Target.Revision, CandidateSequence: review.Candidate.Sequence, ExtractorVersion: f.input.ExtractorVersion, Body: f.body})
					require.NoError(t, err)
					require.Equal(t, "corroborated", preview.Evidence.Status)
					return nil
				}))
				detail = uuid.NewString()
			}
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			switch state {
			case "post_changed":
				_, err := raw.Exec("UPDATE source_posts SET revision=revision+1 WHERE uuid=?", f.target.PostUUID)
				require.NoError(t, err)
			case "source_changed":
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					collection, err := f.repo.SourceCollection.Find(ctx, f.listing.CollectionUUID)
					if err != nil {
						return err
					}
					definition := collection.SourceCollectionDefinition
					definition.State = "disabled"
					_, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
					return err
				}))
			case "identifier_in_use":
				sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, "")
			}
			review := f.review(t)
			require.NotEmpty(t, review.Blockers)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.DiscoveryMatch.PreparePublication(ctx, models.DiscoveryPublicationInput{TargetUUID: f.target.UUID, ExpectedTargetRevision: review.Target.Revision, DetailJobUUID: detail})
				require.ErrorIs(t, err, models.ErrDiscoveryConflict)
				return nil
			}))
			worker := ingest.NewDiscoveryPublicationWorker(f.service)
			worker.Now = func() time.Time { return f.now }
			progress, err := worker.Process(t.Context())
			require.NoError(t, err)
			require.Nil(t, progress.Publication)
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_match_publications"))
		})
	}
}

func TestDiscoveryDetailPublicationRollsBackAndRechecksBeforeCommit(t *testing.T) {
	for _, state := range []string{"interrupted", "forged", "post_changed"} {
		t.Run(state, func(t *testing.T) {
			f := newDetailFixture(t)
			completeDetailFixture(t, f)
			finishDetailListing(t, f)
			input := detailPublicationInput(t, f)
			var prepared models.PreparedDiscoveryPublication
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				var err error
				prepared, err = f.repo.DiscoveryMatch.PreparePublication(ctx, input)
				return err
			}))
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			before := map[string][][]any{}
			for _, name := range []string{"source_posts", "source_post_identifiers", "source_captures", "source_post_identifier_evidence", "source_post_url_evidence", "discovery_match_publications", "discovery_published_records", "discovery_detail_results", "discovery_detail_checkpoints"} {
				before[name] = albumJobRows(t, raw, name)
			}
			calls := 0
			err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				if state == "post_changed" {
					txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
						_, _, err := f.db.ExecSQL(ctx, "UPDATE source_posts SET revision=revision+1 WHERE uuid=?", []any{f.target.PostUUID})
						return err
					})
				}
				_, caught := prepared.Publish(ctx, func(ctx context.Context, capture models.SourceCaptureInput, collection *models.SourceCollection) error {
					calls++
					if state == "interrupted" && calls == 2 {
						return errors.New("fixture interruption")
					}
					if state == "forged" {
						title := "not observed"
						capture.Metadata.Title = &title
					}
					return discoveryCaptureWriter(f.discoveryMatchFixture)(ctx, capture, collection)
				}, f.now)
				if state == "interrupted" {
					require.Error(t, caught)
				} else {
					require.NoError(t, caught)
				}
				return nil
			})
			require.Error(t, err)
			for name, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, name), name)
			}
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := prepared.Publish(ctx, discoveryCaptureWriter(f.discoveryMatchFixture), f.now)
				return err
			}))
		})
	}
}

func TestDiscoveryDetailPublicationNegativeLaterResultIsNotIgnored(t *testing.T) {
	f := newDetailFixture(t)
	first := completeDetailFixture(t, f)
	finishDetailListing(t, f)
	value, err := archive.DecodeJSONObject(f.body, archive.MaxEnrichmentTranscriptBytes)
	require.NoError(t, err)
	value["records"] = []any{}
	f.body, err = json.Marshal(value)
	require.NoError(t, err)
	later := completeDetailFixture(t, f)
	require.NotEqual(t, first.JobUUID, later.JobUUID)
	require.Equal(t, "uncorroborated", later.Evidence.Status)
	require.Equal(t, later, f.review(t).Detail)
	require.Contains(t, f.review(t).Blockers, "detail_required")
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryMatch.PreparePublication(ctx, models.DiscoveryPublicationInput{TargetUUID: f.target.UUID, ExpectedTargetRevision: f.input.ExpectedTargetRevision, DetailJobUUID: first.JobUUID})
		require.ErrorIs(t, err, models.ErrDiscoveryConflict)
		return nil
	}))
}

func TestDiscoveryDetailPublicationPreservesEarlierSearchConflicts(t *testing.T) {
	for _, competing := range []bool{false, true} {
		match := newDiscoveryMatchHistoryFixture(t, true)
		previousListing, previousTarget := match.listing, match.target
		match.append(t, discoveryReviewFinalPage(t, match.page, previousListing.InitialCursor, competing))
		recoverDiscoveryFixture(t, match)
		match.append(t, match.page)
		match.advance(t, 0)
		f := attachDetailFixture(t, match)
		completeDetailFixture(t, f)
		finishDetailListing(t, f)
		require.Contains(t, f.review(t).Blockers, "earlier_comparison_pending")
		worker := ingest.NewDiscoveryPublicationWorker(f.service)
		worker.Now = func() time.Time { return f.now }
		progress, err := worker.Process(t.Context())
		require.NoError(t, err)
		require.Nil(t, progress.Publication)
		require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.DiscoveryMatch.Advance(ctx, previousTarget.UUID, 0, f.now)
			return err
		}))
		if competing {
			require.Contains(t, f.review(t).Blockers, "earlier_candidates_differ")
		} else {
			require.Empty(t, f.review(t).Blockers)
		}
		progress, err = worker.Process(t.Context())
		require.NoError(t, err)
		require.Equal(t, !competing, progress.Publication != nil)
	}
}

func TestDiscoveryDetailPublicationAnonymisationRemovesProofAndCaptures(t *testing.T) {
	f := newDetailFixture(t)
	completeDetailFixture(t, f)
	finishDetailListing(t, f)
	publishDetailFixture(t, f)
	path := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, path)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	checked := sqlite.NewDatabase()
	defer checked.Close()
	require.NoError(t, checked.Open(path))
	raw := openRawDB(t, path)
	defer raw.Close()
	for _, name := range []string{"discovery_match_publications", "discovery_detail_jobs", "source_captures"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+name))
	}
}
