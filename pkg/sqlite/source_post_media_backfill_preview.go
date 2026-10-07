package sqlite

import (
	"cmp"
	"context"
	"slices"

	"github.com/stashapp/stash/pkg/models"
)

// PreviewBackfill uses the retained post/file graph. It never guesses a slot
// from a filename, and multiple independently proven media can share a post.
func (s *SourcePostMediaStore) PreviewBackfill(ctx context.Context, postID string) (*models.SourcePostMediaMatchPreview, error) {
	if sourceFileIDs(&postID) != nil {
		return nil, models.ErrSourcePostMediaInvalid
	}
	post, err := currentSourcePost(ctx, postID)
	if err != nil {
		return nil, err
	}
	if post == nil {
		return nil, models.ErrSourcePostMediaInvalid
	}
	ret := &models.SourcePostMediaMatchPreview{PostUUID: post.UUID, PostRevision: post.Revision, PostState: post.State,
		Policy: models.SourcePostMediaCatalogFilesV1, Candidates: []models.SourcePostMediaMatchCandidate{}}
	rows, err := sourceAlbumEvidenceRows(ctx, post.UUID)
	if err != nil {
		return nil, err
	}
	requested := make(map[string]bool)
	for _, row := range rows {
		if row.AttachmentUUID.Valid {
			continue
		}
		requested[row.MediaUUID] = true
		if row.FileUUID.Valid {
			requested[row.FileUUID.String] = true
		}
	}
	identities, err := sourceGalleryIdentities(ctx, requested)
	if err != nil {
		return nil, err
	}
	candidates := make(map[string]*models.SourcePostMediaMatchCandidate)
	checks := make(map[string]*sourceAlbumProofCheck)
	observations := make(map[string]map[string]bool)
	for _, row := range rows {
		if row.AttachmentUUID.Valid {
			continue
		}
		media := identities[row.MediaUUID]
		if !archiveMedia(media) {
			return nil, models.ErrSourcePayloadCorrupt
		}
		candidate := candidates[media.UUID]
		if candidate == nil {
			association, err := s.Association(ctx, post.UUID, media.UUID)
			if err != nil {
				return nil, err
			}
			candidate = &models.SourcePostMediaMatchCandidate{MediaUUID: media.UUID, MediaRevision: media.Revision,
				MediaKind: media.Kind, MediaState: media.State, AssociationState: association.State,
				Decisions: association.Decisions, Proofs: []models.SourcePostMediaMatchProof{}}
			candidates[media.UUID] = candidate
		}
		proof := models.SourcePostMediaMatchProof{SourceAlbumProof: models.SourceAlbumProof{EvidenceUUID: row.UUID, Basis: "catalog-file", Status: "evidence-only"}}
		if row.Basis == "legacy" && row.PostFileUUID.Valid && row.MatchUUID.Valid && row.FileUUID.Valid {
			checked, err := sourceAlbumProof(ctx, row, "catalog-file", media, checks, identities)
			if err != nil {
				return nil, err
			}
			proof.SourceAlbumProof = *checked
			proof.ObservationUUID = checks[checked.MatchUUID].match.ObservationUUID
			if observations[proof.ObservationUUID] == nil {
				observations[proof.ObservationUUID] = make(map[string]bool)
			}
			observations[proof.ObservationUUID][media.UUID] = true
		}
		candidate.Proofs = append(candidate.Proofs, proof)
	}
	// A rejected attachment may not carry a media UUID. Without proof that its
	// scope excludes this candidate, a migration must not override that intent.
	var attachmentReview bool
	if err := dbWrapper.Get(ctx, &attachmentReview, `SELECT EXISTS(SELECT 1 FROM source_post_identities i
CROSS JOIN source_attachments a ON a.post_uuid=i.post_uuid
JOIN attachment_media_links l ON l.attachment_uuid=a.uuid
JOIN attachment_media_decisions d ON d.uuid=l.decision_uuid
WHERE i.canonical_uuid=? AND d.state IN ('unlinked','undecided'))`, post.UUID); err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		for i := range candidate.Proofs {
			proof := &candidate.Proofs[i]
			if proof.Status == "valid" && len(observations[proof.ObservationUUID]) > 1 {
				proof.Status = "ambiguous-observation"
			}
		}
		switch {
		case candidate.AssociationState == "conflict":
			candidate.Status, candidate.Reason = "review", "post-media-conflict"
		case len(candidate.Decisions) > 0:
			candidate.Status, candidate.Reason = "preserved", "explicit-post-choice"
		case post.State != "active":
			candidate.Status, candidate.Reason = "unavailable", "post-unavailable"
		case candidate.MediaState != models.ArchiveEntityActive:
			candidate.Status, candidate.Reason = "unavailable", "media-unavailable"
		case attachmentReview:
			candidate.Status, candidate.Reason = "review", "explicit-attachment-choice"
		case slices.ContainsFunc(candidate.Proofs, func(p models.SourcePostMediaMatchProof) bool { return p.Status == "valid" }):
			candidate.Status = "matched"
		case slices.ContainsFunc(candidate.Proofs, func(p models.SourcePostMediaMatchProof) bool { return p.Status == "ambiguous-observation" }):
			candidate.Status, candidate.Reason = "review", "ambiguous-observation"
		default:
			candidate.Status, candidate.Reason = "review", "no-current-file-proof"
		}
		ret.Candidates = append(ret.Candidates, *candidate)
	}
	slices.SortFunc(ret.Candidates, func(a, b models.SourcePostMediaMatchCandidate) int { return cmp.Compare(a.MediaUUID, b.MediaUUID) })
	ret.Signature, err = sourceSignature("stash-post-media-backfill-v1", ret)
	return ret, err
}

// Iterate canonical identities before applying the cursor. The existence check
// uses each original member's evidence index and stops at its first match.
const sourcePostMediaBackfillPostsQuery = `SELECT root.post_uuid FROM source_post_identities root INDEXED BY source_post_identities_canonical
WHERE root.canonical_uuid>? AND root.post_uuid=root.canonical_uuid AND EXISTS(
 SELECT 1 FROM source_post_identities member
 CROSS JOIN source_media_evidence e INDEXED BY source_media_evidence_post ON e.post_uuid=member.post_uuid
 WHERE member.canonical_uuid=root.post_uuid AND e.attachment_uuid IS NULL
) ORDER BY root.canonical_uuid LIMIT ?`

func (s *SourcePostMediaStore) BackfillPosts(ctx context.Context, after string, limit int) ([]string, error) {
	after, limit, err := sourceDefinitionPage(after, limit)
	if err != nil {
		return nil, models.ErrSourcePostMediaInvalid
	}
	ret := []string{}
	err = dbWrapper.Select(ctx, &ret, sourcePostMediaBackfillPostsQuery, after, limit)
	return ret, err
}
