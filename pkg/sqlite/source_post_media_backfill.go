package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type postMediaBackfillRow struct {
	UUID          string    `db:"uuid"`
	PostUUID      string    `db:"post_uuid"`
	PostRevision  int       `db:"post_revision"`
	Policy        string    `db:"policy"`
	Signature     string    `db:"signature"`
	RequestDigest string    `db:"request_digest"`
	Selected      int       `db:"selected"`
	Preserved     int       `db:"preserved"`
	Review        int       `db:"review"`
	Unavailable   int       `db:"unavailable"`
	CreatedAt     Timestamp `db:"created_at"`
}

func findPostMediaBackfill(ctx context.Context, id string) (*postMediaBackfillRow, error) {
	if sourceFileIDs(&id) != nil {
		return nil, models.ErrSourcePostMediaInvalid
	}
	var ret postMediaBackfillRow
	if err := dbWrapper.Get(ctx, &ret, "SELECT * FROM post_media_backfills WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &ret, nil
}

func (s *SourcePostMediaStore) BackfillResult(ctx context.Context, id string) (*models.SourcePostMediaBackfillResult, error) {
	r, err := findPostMediaBackfill(ctx, id)
	if err != nil || r == nil {
		return nil, err
	}
	ret := &models.SourcePostMediaBackfillResult{UUID: r.UUID, PostUUID: r.PostUUID, Signature: r.Signature,
		Selected: r.Selected, Preserved: r.Preserved, Review: r.Review, Unavailable: r.Unavailable,
		Decisions: []models.SourcePostMediaDecision{}}
	var rows []sourcePostMediaRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT d.* FROM post_media_backfill_decisions b
JOIN post_media_decisions d ON d.uuid=b.decision_uuid WHERE b.backfill_uuid=? ORDER BY d.post_revision`, id); err != nil {
		return nil, err
	}
	if len(rows) != ret.Selected {
		return nil, models.ErrSourcePayloadCorrupt
	}
	for _, row := range rows {
		ret.Decisions = append(ret.Decisions, *row.resolve())
	}
	return ret, nil
}

func (s *SourcePostMediaStore) MatchedEvidence(ctx context.Context, decision string) ([]models.SourcePostMediaMatchedEvidence, error) {
	if sourceFileIDs(&decision) != nil {
		return nil, models.ErrSourcePostMediaInvalid
	}
	ret := []models.SourcePostMediaMatchedEvidence{}
	err := dbWrapper.Select(ctx, &ret, `SELECT evidence_uuid,post_file_uuid,match_uuid FROM post_media_decision_evidence
WHERE decision_uuid=? ORDER BY evidence_uuid LIMIT ?`, decision, maxSourceGalleryMembers+1)
	if err != nil {
		return nil, err
	}
	if len(ret) > maxSourceGalleryMembers {
		return nil, models.ErrSourceAlbumLimit
	}
	return ret, nil
}

func (s *SourcePostMediaStore) Backfill(ctx context.Context, input models.SourcePostMediaBackfillInput) (*models.SourcePostMediaBackfillResult, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if sourceFileIDs(&input.UUID, &input.PostUUID) != nil || !archive.ValidSHA256(input.Signature) {
		return nil, models.ErrSourcePostMediaInvalid
	}
	digest, err := sourceSignature("stash-post-media-backfill-request-v1", input)
	if err != nil {
		return nil, err
	}
	prior, err := findPostMediaBackfill(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.RequestDigest != digest {
			return nil, models.ErrSourcePostMediaReplay
		}
		return s.BackfillResult(ctx, input.UUID)
	}
	preview, err := s.PreviewBackfill(ctx, input.PostUUID)
	if err != nil {
		return nil, err
	}
	if preview.Signature != input.Signature {
		return nil, models.ErrSourcePostMediaConflict
	}
	counts := make(map[string]int)
	for _, candidate := range preview.Candidates {
		counts[candidate.Status]++
	}
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrSourcePostMediaConflict
		}
		return nil
	})
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO post_media_backfills(uuid,post_uuid,post_revision,policy,signature,request_digest,selected,preserved,review,unavailable)
VALUES(?,?,?,?,?,?,?,?,?,?)`, input.UUID, preview.PostUUID, preview.PostRevision, preview.Policy, preview.Signature, digest,
		counts["matched"], counts["preserved"], counts["review"], counts["unavailable"]); err != nil {
		return nil, err
	}
	revision := preview.PostRevision
	proofOwners := make(map[string]string)
	selected := make(map[string]string)
	for _, candidate := range preview.Candidates {
		if candidate.Status != "matched" {
			continue
		}
		id := uuid.NewSHA1(uuid.MustParse(input.UUID), []byte("post-media-backfill\x00"+candidate.MediaUUID)).String()
		decision, err := s.decide(ctx, models.SourcePostMediaInput{UUID: id, PostUUID: preview.PostUUID, MediaUUID: candidate.MediaUUID,
			ExpectedPostRevision: revision, ExpectedMediaRevision: candidate.MediaRevision, State: "linked", Origin: "migration",
			Reason: "Reviewed historical post/file match: " + preview.Policy}, false)
		if err != nil {
			return nil, err
		}
		revision = decision.PostRevision
		selected[candidate.MediaUUID] = decision.UUID
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO post_media_backfill_decisions(backfill_uuid,decision_uuid) VALUES(?,?)", input.UUID, decision.UUID); err != nil {
			return nil, err
		}
		for _, proof := range candidate.Proofs {
			if proof.Status != "valid" {
				continue
			}
			if _, err := dbWrapper.Exec(ctx, `INSERT INTO post_media_decision_evidence(decision_uuid,evidence_uuid,post_file_uuid,match_uuid)
VALUES(?,?,?,?)`, decision.UUID, proof.EvidenceUUID, proof.PostFileUUID, proof.MatchUUID); err != nil {
				return nil, err
			}
			proofOwners[proof.MatchUUID] = candidate.MediaUUID
		}
	}
	// Sync only once after all decisions; repeated full album previews would
	// turn a large post's bounded match into quadratic gallery work.
	if len(selected) > 0 {
		if err := s.syncGallery(ctx, preview.PostUUID); err != nil {
			return nil, err
		}
	}
	mediaRevisions := make(map[string]int)
	for media := range selected {
		entity, err := (&ArchiveEntityStore{}).Find(ctx, media)
		if err != nil {
			return nil, err
		}
		if !archiveMedia(entity) || entity.State != models.ArchiveEntityActive {
			return nil, models.ErrSourcePostMediaConflict
		}
		mediaRevisions[media] = entity.Revision
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		post, err := (&SourceEvidenceStore{}).FindPost(ctx, preview.PostUUID)
		if err != nil {
			return err
		}
		if post == nil || post.Revision != revision || post.State != preview.PostState {
			return models.ErrSourcePostMediaConflict
		}
		for match, owner := range proofOwners {
			check, err := checkSourceAlbumFile(ctx, match)
			if err != nil {
				return err
			}
			if check.status != "valid" || check.owners[0].UUID != owner {
				return models.ErrSourcePostMediaConflict
			}
		}
		for media, id := range selected {
			association, err := s.Association(ctx, preview.PostUUID, media)
			if err != nil {
				return err
			}
			if association.State != "linked" || len(association.Decisions) != 1 || association.Decisions[0].UUID != id || association.MediaRevision != mediaRevisions[media] {
				return models.ErrSourcePostMediaConflict
			}
		}
		return nil
	})
	ret, err := s.BackfillResult(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	complete = true
	return ret, nil
}
