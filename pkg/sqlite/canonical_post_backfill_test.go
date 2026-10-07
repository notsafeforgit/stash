package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestCanonicalPostBackfillPreservesOriginalProofAndReceiptAcrossChainedMerge(t *testing.T) {
	f, a, _ := postBackfillFixture(t)
	originalRequest := postBackfillRequest(previewPostBackfill(t, f.repo, a))
	originalResult, err := applyPostBackfill(f.repo, originalRequest)
	require.NoError(t, err)
	b := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "other-copy"}, "").UUID
	c := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "current-copy"}, "").UUID
	other := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "unrelated"}, "").UUID
	originalProof := albumLegacyEvidence(t, f, b, "second/original.jpg", nil, models.ArchiveImage, 41, 22)
	outsideProof := albumLegacyEvidence(t, f, other, "unrelated/original.jpg", nil, models.ArchiveImage, 41, 22)
	stale := postBackfillRequest(previewPostBackfill(t, f.repo, b))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	originals := map[string][][]any{}
	for _, table := range []string{"source_media_evidence", "source_post_file_evidence", "source_file_matches", "source_file_observations", "files", "scenes", "images"} {
		originals[table] = albumJobRows(t, raw, table)
	}
	sqlite.ConsolidatePostBackfillFixture(t, f.repo, a, b, "")
	sqlite.ConsolidatePostBackfillFixture(t, f.repo, b, c, "")
	preview := previewPostBackfill(t, f.repo, a)
	require.Equal(t, c, preview.PostUUID)
	require.Equal(t, preview, previewPostBackfill(t, f.repo, c))
	require.Len(t, preview.Candidates, 2)
	for _, candidate := range preview.Candidates {
		if candidate.MediaUUID == originalProof.MediaUUID {
			require.Equal(t, "matched", candidate.Status)
			require.Len(t, candidate.Proofs, 1, "unrelated post evidence is excluded")
			require.Equal(t, originalProof.UUID, candidate.Proofs[0].EvidenceUUID)
		} else {
			require.Equal(t, "preserved", candidate.Status, "the original explicit choice remains authoritative")
		}
	}
	_, err = applyPostBackfill(f.repo, stale)
	require.ErrorIs(t, err, models.ErrSourcePostMediaConflict)
	wrongOwner := postBackfillRequest(preview)
	wrongOwner.PostUUID = a
	_, err = applyPostBackfill(f.repo, wrongOwner)
	require.ErrorIs(t, err, models.ErrSourcePostMediaConflict, "a new request must name the reviewed current owner")
	request := postBackfillRequest(preview)
	result, err := applyPostBackfill(f.repo, request)
	require.NoError(t, err)
	require.Equal(t, 1, result.Selected)
	require.Equal(t, 1, result.Preserved)
	require.Len(t, result.Decisions, 1)
	require.Equal(t, c, result.Decisions[0].PostUUID)
	var proofs []models.SourcePostMediaMatchedEvidence
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		proofs, err = f.repo.SourcePostMedia.MatchedEvidence(ctx, result.Decisions[0].UUID)
		return err
	}))
	require.Len(t, proofs, 1)
	require.Equal(t, originalProof.UUID, proofs[0].EvidenceUUID)
	var unrelated struct {
		PostFile string `json:"source_post_file_evidence_uuid"`
		Match    string `json:"source_file_match_uuid"`
	}
	require.NoError(t, json.Unmarshal(outsideProof.Details, &unrelated))
	_, err = raw.Exec(`INSERT INTO post_media_decision_evidence(decision_uuid,evidence_uuid,post_file_uuid,match_uuid) VALUES(?,?,?,?)`,
		result.Decisions[0].UUID, outsideProof.UUID, unrelated.PostFile, unrelated.Match)
	require.ErrorContains(t, err, "original file chain and current post identity", "a shared media/file does not make unrelated post evidence eligible")
	for table, rows := range originals {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	for _, pair := range []struct {
		input  models.SourcePostMediaBackfillInput
		result *models.SourcePostMediaBackfillResult
	}{{originalRequest, originalResult}, {request, result}} {
		replay, err := applyPostBackfill(f.repo, pair.input)
		require.NoError(t, err)
		require.Equal(t, pair.result, replay)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	// Startup must reject a well-formed but unrelated original chain even if an
	// external writer bypassed both immutable and insertion guards.
	require.NoError(t, f.db.Close())
	var immutable string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='post_media_decision_evidence_immutable'").Scan(&immutable))
	_, err = raw.Exec("DROP TRIGGER post_media_decision_evidence_immutable")
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE post_media_decision_evidence SET evidence_uuid=?,post_file_uuid=?,match_uuid=? WHERE decision_uuid=?", outsideProof.UUID, unrelated.PostFile, unrelated.Match, result.Decisions[0].UUID)
	require.NoError(t, err)
	_, err = raw.Exec(immutable)
	require.NoError(t, err)
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.ErrorContains(t, f.db.Open(f.db.DatabasePath()), "invalid historical post media backfills")
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after, "startup rejection does not alter the database")
}

func TestCanonicalAlbumBackfillSharesOriginalFilenameProofAndPreservesAttachmentOwner(t *testing.T) {
	f, a, attachment := singleBackfillFixture(t)
	b := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "current-post"}, "").UUID
	var selection *models.AttachmentSelection
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		selection, err = f.repo.SourceAttachment.Selection(ctx, a)
		return err
	}))
	stale := albumBackfillPreview(t, f.repo, a, models.SourceAlbumRedditFilenameV1)
	sqlite.ConsolidatePostBackfillFixture(t, f.repo, a, b, *selection.Decision.CaptureUUID)
	preview := albumBackfillPreview(t, f.repo, a, models.SourceAlbumRedditFilenameV1)
	require.Equal(t, b, preview.PostUUID)
	require.Len(t, preview.Matches, 1)
	require.Equal(t, "matched", preview.Matches[0].Status)
	require.Equal(t, attachment, preview.Matches[0].AttachmentUUID)
	for _, signature := range []string{stale.Signature, preview.Signature} {
		err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.SourceGallery.Backfill(ctx, a, preview.Policy, signature)
			return err
		})
		require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
	}
	result := applyAlbumBackfill(t, f.repo, preview)
	require.Equal(t, 1, result.Selected)
	require.True(t, result.Gallery.Created)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	var original string
	require.NoError(t, raw.QueryRow("SELECT post_uuid FROM source_media_evidence WHERE attachment_uuid=?", attachment).Scan(&original))
	require.Equal(t, a, original, "derived attachment evidence keeps the original attachment owner")
	sourceGalleryMemberships(t, f.repo, *result.Gallery.GalleryID, nil, []int{31})
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, "sync", albumBackfillPreview(t, f.db.Repository(), a, preview.Policy).Gallery.Action)
}

func TestCanonicalAlbumJobsRejectUnpublishedPlansAndResumeOriginalPublication(t *testing.T) {
	for _, published := range []bool{false, true} {
		t.Run(map[bool]string{false: "unpublished", true: "published"}[published], func(t *testing.T) {
			f, a, _ := singleBackfillFixture(t)
			b := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "survivor"}, "").UUID
			s := gallery.NewAlbumBackfill(f.repo)
			input := albumJobInput(t, s, a)
			admitted, err := s.Submit(t.Context(), input)
			require.NoError(t, err)
			var publication gallery.AlbumPublication
			worker := gallery.NewAlbumWorker(s, func(_ context.Context, result gallery.AlbumPublication, _ gallery.AlbumEffectGuard) error {
				publication = result
				return errors.New("delivery unavailable")
			})
			if published {
				processAlbumJob(t, worker)
				status := albumJobStatus(t, s, admitted.JobUUID)
				require.True(t, status.PublicationCommitted)
			}
			// Leave selection/gallery heads unsettled to prove that a committed
			// notification retry never reopens current domain publication.
			sqlite.ConsolidatePostBackfillFixture(t, f.repo, a, b, "")
			worker.Effects = func(ctx context.Context, result gallery.AlbumPublication, guard gallery.AlbumEffectGuard) error {
				if !published {
					t.Fatal("stale unpublished work reached effects")
				}
				require.Equal(t, publication, result)
				require.Equal(t, a, result.PostUUID)
				return guard(ctx)
			}
			if published {
				status := albumJobStatus(t, s, admitted.JobUUID)
				cancelled, err := s.Cancel(t.Context(), status.JobUUID, status.Revision)
				require.NoError(t, err)
				resumed, err := s.Retry(t.Context(), cancelled.JobUUID, cancelled.Revision, uuid.NewString())
				require.NoError(t, err)
				require.Equal(t, &publication, resumed.Publication)
				processAlbumJob(t, worker)
				require.True(t, albumJobStatus(t, s, resumed.JobUUID).HooksFinished)
			} else {
				processAlbumJob(t, worker)
				status := albumJobStatus(t, s, admitted.JobUUID)
				require.Equal(t, "failed", status.State)
				require.Equal(t, "album_preview_changed", status.ErrorCode)
				require.False(t, status.PublicationCommitted)
			}
			replay, err := s.Submit(t.Context(), input)
			require.NoError(t, err)
			require.Equal(t, admitted.JobUUID, replay.JobUUID)
			require.Equal(t, a, replay.PostUUID)
		})
	}
}
