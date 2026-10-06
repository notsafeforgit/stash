package sqlite_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func postBackfillFixture(t *testing.T) (sourceFileFixture, string, *models.SourceMediaEvidence) {
	t.Helper()
	f := newSourceFileFixture(t)
	recordContentClaim(t, f.repo, f.claim)
	post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "original-post"}, "")
	evidence := albumLegacyEvidence(t, f, post.UUID, "original/arbitrary vendor filename.gif", nil, models.ArchiveScene, 31, 21)
	return f, post.UUID, evidence
}

func previewPostBackfill(t *testing.T, repo models.Repository, post string) *models.SourcePostMediaMatchPreview {
	t.Helper()
	var ret *models.SourcePostMediaMatchPreview
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourcePostMedia.PreviewBackfill(ctx, post)
		return err
	}))
	return ret
}

func postBackfillRequest(preview *models.SourcePostMediaMatchPreview) models.SourcePostMediaBackfillInput {
	return models.SourcePostMediaBackfillInput{UUID: uuid.NewString(), PostUUID: preview.PostUUID, Signature: preview.Signature}
}

func applyPostBackfill(repo models.Repository, input models.SourcePostMediaBackfillInput) (*models.SourcePostMediaBackfillResult, error) {
	var ret *models.SourcePostMediaBackfillResult
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourcePostMedia.Backfill(ctx, input)
		return err
	})
	return ret, err
}

func TestPostMediaBackfillSelectsHistoricalCapturesWithoutInventingAlbums(t *testing.T) {
	f, post, original := postBackfillFixture(t)
	capture := recordSourceTestCapture(t, f.repo, sourceTestCapture(t, post, 1, "Original profile"))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, CaptureUUID: capture.UUID})
	}))
	albumLegacyEvidence(t, f, post, "another/original caption.gif", nil, models.ArchiveScene, 31, 21)
	albumLegacyEvidence(t, f, post, "album image with no embedded identifiers.jpg", nil, models.ArchiveImage, 41, 22)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "files", "source_captures", "source_media_evidence", "source_post_file_evidence", "source_file_matches"} {
		before[table] = albumJobRows(t, raw, table)
	}
	preview := previewPostBackfill(t, f.repo, post)
	require.Equal(t, preview, previewPostBackfill(t, f.repo, post))
	require.Len(t, preview.Candidates, 2)
	for _, candidate := range preview.Candidates {
		require.Equal(t, "matched", candidate.Status)
		for _, proof := range candidate.Proofs {
			require.Equal(t, "valid", proof.Status)
			require.Equal(t, "catalog-file", proof.Basis)
			require.NotEmpty(t, proof.ObservationUUID)
		}
	}
	input := postBackfillRequest(preview)
	result, err := applyPostBackfill(f.repo, input)
	require.NoError(t, err)
	require.Equal(t, 2, result.Selected)
	require.Len(t, result.Decisions, 2)
	for _, decision := range result.Decisions {
		require.Equal(t, "migration", decision.Origin)
		require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			proofs, err := f.repo.SourcePostMedia.MatchedEvidence(ctx, decision.UUID)
			require.NoError(t, err)
			require.NotEmpty(t, proofs)
			if decision.MediaUUID == original.MediaUUID {
				require.Len(t, proofs, 2)
			}
			rows, err := f.repo.MetadataPolicy.SampleSources(ctx, models.MetadataPolicySampleScope{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, EntityUUID: decision.MediaUUID}, nil, 100)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, capture.UUID, rows[0].CaptureUUID)
			require.Equal(t, decision.UUID, rows[0].PostMediaDecisionUUID)
			require.Empty(t, rows[0].AttachmentUUID)
			return nil
		}))
	}
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	for _, table := range []string{"galleries", "source_attachments", "source_attachment_manifests", "media_contents"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	// Later review and restart must not turn a lost response into another link.
	_, err = applyPostMedia(f.repo, postMediaInput(t, f.repo, post, original.MediaUUID, "unlinked"))
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	replay, err := applyPostBackfill(f.repo, input)
	require.NoError(t, err)
	require.Equal(t, result, replay)
	require.Equal(t, "unlinked", postMediaAssociation(t, f.repo, post, original.MediaUUID).State)
	changed := input
	changed.Signature = previewPostBackfill(t, f.repo, post).Signature
	_, err = applyPostBackfill(f.repo, changed)
	require.ErrorIs(t, err, models.ErrSourcePostMediaReplay)
	changed.UUID = uuid.NewString()
	noop, err := applyPostBackfill(f.repo, changed)
	require.NoError(t, err)
	require.Zero(t, noop.Selected)
	require.Equal(t, 2, noop.Preserved)
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	anonymous := openRawDB(t, output)
	defer anonymous.Close()
	for _, table := range []string{"post_media_backfills", "post_media_backfill_decisions", "post_media_decision_evidence"} {
		require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestPostMediaBackfillPreservesExplicitChoicesAndUnverifiedEvidence(t *testing.T) {
	for _, choice := range []string{"linked", "unlinked", "undecided", "file-changed", "evidence-only", "attachment-unlinked", "attachment-undecided"} {
		t.Run(choice, func(t *testing.T) {
			f, post, evidence := postBackfillFixture(t)
			status := "preserved"
			switch choice {
			case "file-changed":
				attachmentSQL(t, f.db, "UPDATE files SET size=1001 WHERE id=21")
				status = "review"
			case "evidence-only":
				// An unrelated media claim has no file proof and cannot be selected.
				other := archiveFind(t, f.repo, models.ArchiveScene, 32)
				recordMediaEvidence(t, f.repo, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post, MediaUUID: other.UUID, Basis: "legacy"})
			case "attachment-unlinked", "attachment-undecided":
				selection := selectAlbum(t, f.repo, post, models.SourceAttachmentManifestInput{DeclaredAlbum: true, Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "clip")}})
				a := selection.Entries[0].Attachment
				state := "unlinked"
				if choice == "attachment-undecided" {
					state = "undecided"
				}
				require.NoError(t, applyMediaChoice(f.repo, models.AttachmentMediaDecisionInput{AttachmentUUID: a.UUID, ExpectedAttachmentRevision: a.Revision, State: state, Origin: "review"}))
				status = "review"
			default:
				_, err := applyPostMedia(f.repo, postMediaInput(t, f.repo, post, evidence.MediaUUID, choice))
				require.NoError(t, err)
			}
			preview := previewPostBackfill(t, f.repo, post)
			if choice == "evidence-only" {
				require.Len(t, preview.Candidates, 2)
				result, err := applyPostBackfill(f.repo, postBackfillRequest(preview))
				require.NoError(t, err)
				require.Equal(t, 1, result.Selected)
				require.Equal(t, 1, result.Review)
				return
			}
			require.Equal(t, status, preview.Candidates[0].Status)
			result, err := applyPostBackfill(f.repo, postBackfillRequest(preview))
			require.NoError(t, err)
			require.Zero(t, result.Selected)
			if status == "preserved" {
				require.Equal(t, choice, postMediaAssociation(t, f.repo, post, evidence.MediaUUID).State)
			}
		})
	}
}

func TestPostMediaBackfillRejectsStaleProofAndRollsBackLaterChanges(t *testing.T) {
	for _, when := range []string{"before-preview-apply", "after-apply-before-commit", "media-changed-before-commit", "ignored-write-error"} {
		t.Run(when, func(t *testing.T) {
			f, post, _ := postBackfillFixture(t)
			preview := previewPostBackfill(t, f.repo, post)
			input := postBackfillRequest(preview)
			switch when {
			case "before-preview-apply":
				attachmentSQL(t, f.db, "UPDATE files SET size=1001 WHERE id=21")
				_, err := applyPostBackfill(f.repo, input)
				require.ErrorIs(t, err, models.ErrSourcePostMediaConflict)
			case "after-apply-before-commit", "media-changed-before-commit":
				err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					_, err := f.repo.SourcePostMedia.Backfill(ctx, input)
					require.NoError(t, err)
					change := "UPDATE files SET size=1001 WHERE id=21"
					if when == "media-changed-before-commit" {
						change = "UPDATE scenes SET title='Changed after review' WHERE id=31"
					}
					_, _, err = f.db.ExecSQL(ctx, change, nil)
					return err
				})
				require.ErrorIs(t, err, models.ErrSourcePostMediaConflict)
			case "ignored-write-error":
				attachmentSQL(t, f.db, `CREATE TRIGGER fail_post_media_proof BEFORE INSERT ON post_media_decision_evidence BEGIN SELECT RAISE(ABORT,'simulated proof failure'); END`)
				err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					_, err := f.repo.SourcePostMedia.Backfill(ctx, input)
					require.ErrorContains(t, err, "simulated proof failure")
					return nil
				})
				require.ErrorIs(t, err, models.ErrSourcePostMediaConflict)
			}
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			for _, table := range []string{"post_media_backfills", "post_media_backfill_decisions", "post_media_decision_evidence", "post_media_decisions", "post_media_links"} {
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
			}
		})
	}
}

func TestPostMediaBackfillReviewsCompetingMatchesForOneObservation(t *testing.T) {
	f, post, evidence := postBackfillFixture(t)
	var details map[string]string
	require.NoError(t, json.Unmarshal(evidence.Details, &details))
	attachmentSQL(t, f.db, `INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at) VALUES(22,1,'other.mp4',1000,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`)
	attachmentSQL(t, f.db, `INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(32,22,1)`)
	file := archiveFind(t, f.repo, models.ArchiveFile, 22)
	media := archiveFind(t, f.repo, models.ArchiveScene, 32)
	preview := previewPostBackfill(t, f.repo, post)
	match := recordFileMatch(t, f.repo, models.SourceFileMatch{UUID: uuid.NewString(), ObservationUUID: preview.Candidates[0].Proofs[0].ObservationUUID, FileUUID: file.UUID, Generation: 1, Basis: "review", Origin: "review"})
	details["source_file_match_uuid"] = match.UUID
	body, err := json.Marshal(details)
	require.NoError(t, err)
	recordMediaEvidence(t, f.repo, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post, MediaUUID: media.UUID, FileUUID: &file.UUID, Basis: "legacy", Details: body})
	preview = previewPostBackfill(t, f.repo, post)
	require.Len(t, preview.Candidates, 2)
	for _, candidate := range preview.Candidates {
		require.Equal(t, "review", candidate.Status)
		require.Equal(t, "ambiguous-observation", candidate.Reason)
	}
	result, err := applyPostBackfill(f.repo, postBackfillRequest(preview))
	require.NoError(t, err)
	require.Zero(t, result.Selected)
	require.Equal(t, 2, result.Review)
}

func TestPostMediaBackfillFollowsMergedMediaWithoutRecreatingIt(t *testing.T) {
	f, post, original := postBackfillFixture(t)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if _, _, err := f.db.ExecSQL(ctx, `INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(32,21,1)`, nil); err != nil {
			return err
		}
		if err := f.repo.Scene.RedirectMergedIdentities(ctx, []int{31}, 32); err != nil {
			return err
		}
		return f.repo.Scene.Destroy(ctx, 31)
	}))
	preview := previewPostBackfill(t, f.repo, post)
	canonical := archiveFind(t, f.repo, models.ArchiveScene, 32)
	require.Equal(t, canonical.UUID, preview.Candidates[0].MediaUUID)
	require.Equal(t, "matched", preview.Candidates[0].Status)
	result, err := applyPostBackfill(f.repo, postBackfillRequest(preview))
	require.NoError(t, err)
	require.Equal(t, 1, result.Selected)
	require.Equal(t, canonical.UUID, result.Decisions[0].MediaUUID)
	require.Equal(t, "linked", postMediaAssociation(t, f.repo, post, original.MediaUUID).State)
}
