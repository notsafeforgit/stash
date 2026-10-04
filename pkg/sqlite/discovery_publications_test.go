package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

func removeDiscoveryPublicationSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeDiscoveryRecoverySchema(t, raw)
	_, err := raw.Exec(`DROP TABLE discovery_published_records; DROP TABLE discovery_match_publications;
 DELETE FROM native_migration_history WHERE version=1000074`)
	require.NoError(t, err)
}

func prepareDiscoveryPublication(t *testing.T, f *discoveryMatchFixture) models.PreparedDiscoveryPublication {
	t.Helper()
	var prepared models.PreparedDiscoveryPublication
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		target, err := f.repo.DiscoveryMatch.Target(ctx, f.target.UUID)
		if err != nil {
			return err
		}
		prepared, err = f.repo.DiscoveryMatch.PreparePublication(ctx, models.DiscoveryPublicationInput{TargetUUID: target.UUID, ExpectedTargetRevision: target.Revision})
		return err
	}))
	require.NotNil(t, prepared)
	return prepared
}

func discoveryCaptureWriter(f *discoveryMatchFixture) models.DiscoveryCaptureWriter {
	return func(ctx context.Context, input models.SourceCaptureInput, collection *models.SourceCollection) error {
		capture, err := f.repo.SourceEvidence.RecordCapture(ctx, input)
		if err != nil {
			return err
		}
		return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, CaptureUUID: capture.UUID, CreatedAt: f.now})
	}
}

func newDiscoveryPublicationFixture(t *testing.T) *discoveryMatchFixture {
	t.Helper()
	f := newDiscoveryMatchFixture(t)
	f.append(t, discoveryReviewFinalPage(t, f.page, nil, false))
	f.advance(t, 0)
	return f
}

func TestDiscoveryPublicationIsAtomicReplayableAndPreservesNativeCaptures(t *testing.T) {
	f := newDiscoveryPublicationFixture(t)
	one, two := prepareDiscoveryPublication(t, f), prepareDiscoveryPublication(t, f)
	results := make([]*models.DiscoveryMatchPublication, 2)
	failures := make([]error, 2)
	var writes atomic.Int32
	var wg sync.WaitGroup
	for i, prepared := range []models.PreparedDiscoveryPublication{one, two} {
		wg.Go(func() {
			failures[i] = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				var err error
				results[i], err = prepared.Publish(ctx, func(ctx context.Context, input models.SourceCaptureInput, collection *models.SourceCollection) error {
					writes.Add(1)
					return discoveryCaptureWriter(f)(ctx, input, collection)
				}, f.now)
				return err
			})
		})
	}
	wg.Wait()
	for _, err := range failures {
		require.NoError(t, err)
	}
	require.Equal(t, results[0], results[1])
	require.EqualValues(t, 3, writes.Load(), "concurrent callers share one original publication")
	publication := results[0]
	require.Equal(t, 3, publication.RecordCount)
	require.Equal(t, 3, publication.CaptureCount)
	require.Equal(t, "native:reddit", publication.Namespace)
	require.Equal(t, "abc123", publication.Value)
	require.Equal(t, "exact-title-and-date", publication.Basis)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		post, err := f.repo.SourceEvidence.FindPostByIdentifier(ctx, models.SourcePostIdentifier{Namespace: publication.Namespace, Value: publication.Value})
		require.NoError(t, err)
		require.Equal(t, f.target.PostUUID, post.UUID)
		first, err := f.repo.DiscoveryMatch.PublishedRecords(ctx, f.target.UUID, -1, 1)
		require.NoError(t, err)
		require.Len(t, first, 1)
		require.Zero(t, first[0].Ordinal)
		rest, err := f.repo.DiscoveryMatch.PublishedRecords(ctx, f.target.UUID, first[0].Ordinal, 100)
		require.NoError(t, err)
		require.Len(t, rest, 2)
		for _, record := range append(first, rest...) {
			capture, err := f.repo.SourceEvidence.FindCapture(ctx, record.CaptureUUID)
			require.NoError(t, err)
			require.Equal(t, "Album with source positions", *capture.Metadata.Title)
			require.Equal(t, f.target.PostUUID, capture.PostUUID)
		}
		return nil
	}))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM discovery_pages"), "publication does not release source evidence")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	_, err := raw.Exec("UPDATE source_posts SET state='forgotten',revision=revision+1 WHERE uuid=?", f.target.PostUUID)
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	review := f.review(t)
	require.Equal(t, publication, review.Publication)
	require.Empty(t, review.Blockers, "a later native edit does not undo an accepted publication")
	replayed := prepareDiscoveryPublication(t, f)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		result, err := replayed.Publish(ctx, nil, f.now)
		require.Equal(t, publication, result, "original receipt survives a later forgotten post")
		return err
	}))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryMatch.PreparePublication(ctx, models.DiscoveryPublicationInput{TargetUUID: f.target.UUID, ExpectedTargetRevision: publication.TargetRevision + 1})
		require.ErrorIs(t, err, models.ErrDiscoveryConflict)
		return nil
	}))
}

func TestDiscoveryPublicationRollsBackCaughtFailuresAndForgedCaptures(t *testing.T) {
	for _, mode := range []string{"interrupted", "forged", "source_changed"} {
		t.Run(mode, func(t *testing.T) {
			f := newDiscoveryPublicationFixture(t)
			prepared := prepareDiscoveryPublication(t, f)
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			before := map[string][][]any{}
			for _, table := range []string{"source_posts", "source_post_identifiers", "source_captures", "source_post_revisions", "source_collection_captures", "source_post_identifier_evidence", "source_post_urls", "source_post_url_evidence", "discovery_match_publications", "discovery_published_records"} {
				before[table] = albumJobRows(t, raw, table)
			}
			calls := 0
			err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				if mode == "source_changed" {
					txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
						collection, err := f.repo.SourceCollection.Find(ctx, f.listing.CollectionUUID)
						if err != nil {
							return err
						}
						definition := collection.SourceCollectionDefinition
						definition.State = "disabled"
						_, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
						return err
					})
				}
				_, caught := prepared.Publish(ctx, func(ctx context.Context, input models.SourceCaptureInput, collection *models.SourceCollection) error {
					calls++
					if mode == "interrupted" && calls == 2 {
						return errors.New("fixture interruption")
					}
					if mode == "forged" {
						wrong := "not in the source observation"
						input.Metadata.Title = &wrong
					}
					return discoveryCaptureWriter(f)(ctx, input, collection)
				}, f.now)
				if mode == "interrupted" {
					require.Error(t, caught)
				} else {
					require.NoError(t, caught)
				}
				return nil // a caught error must not commit a partial identity
			})
			switch mode {
			case "interrupted":
				require.ErrorIs(t, err, models.ErrDiscoveryAtomic)
			case "forged":
				require.ErrorIs(t, err, models.ErrSourcePayloadCorrupt)
			case "source_changed":
				require.ErrorIs(t, err, models.ErrDiscoveryConflict)
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := prepared.Publish(ctx, discoveryCaptureWriter(f), f.now)
				return err
			}), "the original prepared operation can retry after rollback")
		})
	}
}

func TestDiscoveryPublicationRechecksNativeEditsAndIdentifierCollisions(t *testing.T) {
	f := newDiscoveryPublicationFixture(t)
	prepared := prepareDiscoveryPublication(t, f)
	other := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, "")
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := prepared.Publish(ctx, discoveryCaptureWriter(f), f.now)
		return err
	})
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		post, err := f.repo.SourceEvidence.FindPostByIdentifier(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"})
		require.NoError(t, err)
		require.Equal(t, other.UUID, post.UUID, "publication cannot implicitly consolidate posts")
		result, err := f.repo.DiscoveryMatch.Publication(ctx, f.target.UUID)
		require.NoError(t, err)
		require.Nil(t, result)
		_, err = f.repo.DiscoveryMatch.PreparePublication(ctx, models.DiscoveryPublicationInput{TargetUUID: uuid.NewString(), ExpectedTargetRevision: 2})
		require.ErrorIs(t, err, models.ErrDiscoveryConflict)
		return nil
	}))
}

func TestDiscoveryPublicationUsesTheCorroboratingObservationTime(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	page, err := archive.DecodeJSONObject(f.page, archive.MaxDiscoveryPageBytes)
	require.NoError(t, err)
	page["complete"], page["next_cursor"] = true, nil
	page["records"].([]any)[1].(map[string]any)["patch"].(map[string]any)["date"] = "2026-10-03"
	body, err := archive.EncodeSourceJSON(page)
	require.NoError(t, err)
	f.append(t, body)
	f.advance(t, 0)
	prepared := prepareDiscoveryPublication(t, f)
	var publication *models.DiscoveryMatchPublication
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		publication, err = prepared.Publish(ctx, discoveryCaptureWriter(f), f.now)
		return err
	}))
	require.Equal(t, 1, publication.WitnessOrdinal, "the earlier title-only record did not establish identity")
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		links, err := f.repo.SourcePostLinks.IdentifierEvidence(ctx, f.target.PostUUID, "", 100)
		require.NoError(t, err)
		for _, link := range links {
			if link.UUID == publication.EvidenceUUID {
				parsed, err := archive.ParseDiscoveryPage(body)
				require.NoError(t, err)
				require.Equal(t, parsed.Records[1].ObservedAt, link.ObservedAt.Format("2006-01-02T15:04:05.999999999Z07:00"))
				return nil
			}
		}
		t.Fatal("missing accepted identity evidence")
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestDiscoveryPublicationCannotBypassIncompleteWeakOrCompetingEvidence(t *testing.T) {
	for _, state := range []string{"unfinished", "uncompared", "weak", "competing", "empty"} {
		t.Run(state, func(t *testing.T) {
			f := newDiscoveryMatchFixture(t)
			var body json.RawMessage
			switch state {
			case "unfinished":
				body = f.page
			case "weak":
				page, err := archive.DecodeJSONObject(f.page, archive.MaxDiscoveryPageBytes)
				require.NoError(t, err)
				page["complete"], page["next_cursor"] = true, nil
				body, err = archive.EncodeSourceJSON(page)
				require.NoError(t, err)
			case "empty":
				body = listingContinuation(t, f.page, nil, nil, true)
			default:
				body = discoveryReviewFinalPage(t, f.page, nil, state == "competing")
			}
			f.append(t, body)
			if state != "uncompared" {
				f.advance(t, 0)
			}
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.DiscoveryMatch.PreparePublication(ctx, models.DiscoveryPublicationInput{TargetUUID: f.target.UUID, ExpectedTargetRevision: 2})
				require.ErrorIs(t, err, models.ErrDiscoveryConflict)
				return nil
			}))
			worker := ingest.NewDiscoveryPublicationWorker(ingest.New(f.repo))
			worker.Now = func() time.Time { return f.now }
			progress, err := worker.Process(t.Context())
			require.NoError(t, err, "blocked targets stay in review without interrupting the worker")
			require.Nil(t, progress.Publication)
			require.False(t, progress.HasMore)
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_match_publications"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_post_identifiers WHERE value='abc123'"))
		})
	}
}
