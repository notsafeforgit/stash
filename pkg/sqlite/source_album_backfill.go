package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// Only this post's indexed evidence is inspected. A bounded but incomplete
// candidate set must never appear unique, so exceeding the limit is an error.
type sourceAlbumEvidence struct {
	UUID            string         `db:"uuid"`
	AttachmentUUID  sql.NullString `db:"attachment_uuid"`
	MediaUUID       string         `db:"media_uuid"`
	FileUUID        sql.NullString `db:"file_uuid"`
	Basis           string         `db:"basis"`
	PostFileUUID    sql.NullString `db:"post_file_uuid"`
	MatchUUID       sql.NullString `db:"match_uuid"`
	RelativePath    sql.NullString `db:"relative_path"`
	SourceMediaID   sql.NullString `db:"source_media_id"`
	SourceMediaType sql.NullString `db:"source_media_type"`
}

func sourceAlbumEvidenceRows(ctx context.Context, post string) ([]sourceAlbumEvidence, error) {
	var rows []sourceAlbumEvidence
	err := dbWrapper.Select(ctx, &rows, `SELECT m.uuid,m.attachment_uuid,m.media_uuid,m.file_uuid,m.basis,
 p.uuid AS post_file_uuid,f.uuid AS match_uuid,o.relative_path,
 CASE WHEN json_type(p.details,'$.source_media_id')='text' THEN json_extract(p.details,'$.source_media_id') END AS source_media_id,
 json_type(p.details,'$.source_media_id') AS source_media_type
 FROM source_media_evidence m
 LEFT JOIN source_post_file_evidence p ON m.attachment_uuid IS NULL AND m.basis='legacy'
  AND p.uuid=json_extract(m.details,'$.source_post_file_evidence_uuid') AND p.post_uuid=m.post_uuid
  AND p.origin='migration' AND p.basis='catalog-appearance'
 LEFT JOIN source_file_observations o ON o.uuid=p.observation_uuid
 LEFT JOIN source_file_matches f ON f.uuid=json_extract(m.details,'$.source_file_match_uuid') AND f.observation_uuid=p.observation_uuid
 WHERE m.post_uuid=? AND (m.attachment_uuid IS NULL OR NOT EXISTS
  (SELECT 1 FROM attachment_media_links l WHERE l.attachment_uuid=m.attachment_uuid))
 ORDER BY m.uuid LIMIT ?`, post, maxSourceGalleryMembers+1)
	if err != nil {
		return nil, err
	}
	if len(rows) > maxSourceGalleryMembers {
		return nil, models.ErrSourceAlbumLimit
	}
	return rows, nil
}

func sourceAlbumPostIdentifiers(ctx context.Context, post string) ([]models.SourcePostIdentifier, error) {
	var rows []models.SourcePostIdentifier
	err := dbWrapper.Select(ctx, &rows, `SELECT namespace,value FROM source_post_identifiers WHERE post_uuid=? ORDER BY namespace,value LIMIT ?`, post, maxSourceGalleryMembers+1)
	if err != nil {
		return nil, err
	}
	if len(rows) > maxSourceGalleryMembers {
		return nil, models.ErrSourceAlbumLimit
	}
	return rows, nil
}

func sourceAlbumASCIIID(value string, upper bool) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (upper && c >= 'A' && c <= 'Z') {
			continue
		}
		return false
	}
	return true
}

func sourceAlbumEvidenceBasis(row sourceAlbumEvidence, attachment models.SourceAttachment, posts []models.SourcePostIdentifier, policy string) string {
	if row.AttachmentUUID.Valid {
		if row.AttachmentUUID.String == attachment.UUID {
			return "attachment-evidence"
		}
		return ""
	}
	if row.Basis != "legacy" || !row.PostFileUUID.Valid || !row.MatchUUID.Valid || !row.FileUUID.Valid {
		return ""
	}
	ref := attachment.Reference
	prefix := map[string]string{"native:reddit": "reddit:media:", "native:twitter": "twitter:media:"}[ref.Namespace]
	for _, post := range posts {
		if post.Namespace != ref.Namespace {
			continue
		}
		if prefix != "" && row.SourceMediaID.Valid && row.SourceMediaID.String == prefix+ref.Value {
			return "source-id"
		}
		// A conflicting or malformed explicit ID cannot fall back to a name.
		if row.SourceMediaType.Valid && row.SourceMediaType.String != "null" {
			continue
		}
		if policy != models.SourceAlbumRedditFilenameV1 || ref.Namespace != "native:reddit" ||
			!sourceAlbumASCIIID(post.Value, false) || !sourceAlbumASCIIID(ref.Value, true) {
			continue
		}
		// Use the original observation, not its deduplicated/converted survivor.
		// Downloader numbering and the title suffix are deliberately irrelevant.
		name, prefix := path.Base(row.RelativePath.String), post.Value+"_"+ref.Value
		if strings.HasPrefix(name, prefix+"_") || strings.HasPrefix(name, prefix+".") {
			return "legacy-reddit-filename"
		}
	}
	return ""
}

type sourceAlbumProofCheck struct {
	match  *models.SourceFileMatch
	owners []*models.ArchiveEntity
	status string
}

func checkSourceAlbumFile(ctx context.Context, id string) (*sourceAlbumProofCheck, error) {
	match, err := sourceFileFind[models.SourceFileMatch](ctx, id, "SELECT "+sourceFileMatchColumns+" FROM source_file_matches")
	if err != nil {
		return nil, err
	}
	if match == nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	ret := &sourceAlbumProofCheck{match: match, status: "valid"}
	if err := (&SourceFileStore{}).validateMatch(ctx, match); err != nil {
		if errors.Is(err, models.ErrFileGenerationConflict) || errors.Is(err, models.ErrFileContentConflict) || errors.Is(err, models.ErrSourceFileEvidenceInvalid) {
			ret.status = "file-changed"
			return ret, nil
		}
		return nil, err
	}
	ret.owners, err = (&FileContentStore{}).Owners(ctx, match.FileUUID)
	if err != nil {
		return nil, err
	}
	if len(ret.owners) != 1 {
		ret.status = "owner-changed"
	}
	return ret, nil
}

func sourceAlbumProof(ctx context.Context, row sourceAlbumEvidence, basis string, media *models.ArchiveEntity, cache map[string]*sourceAlbumProofCheck, identities map[string]*models.ArchiveEntity) (*models.SourceAlbumProof, error) {
	ret := &models.SourceAlbumProof{EvidenceUUID: row.UUID, Basis: basis, Status: "evidence-only"}
	if basis == "attachment-evidence" {
		return ret, nil
	}
	ret.PostFileUUID, ret.MatchUUID, ret.RelativePath = row.PostFileUUID.String, row.MatchUUID.String, row.RelativePath.String
	check := cache[ret.MatchUUID]
	if check == nil {
		var err error
		check, err = checkSourceAlbumFile(ctx, ret.MatchUUID)
		if err != nil {
			return nil, err
		}
		cache[ret.MatchUUID] = check
	}
	match := check.match
	ret.FileUUID, ret.Generation, ret.ArchiveFileUUID, ret.ArchiveGeneration = match.FileUUID, match.Generation, match.ArchiveFileUUID, match.ArchiveGeneration
	ret.Status = check.status
	file := identities[row.FileUUID.String]
	if file == nil || file.UUID != match.FileUUID || file.State != models.ArchiveEntityActive {
		ret.Status = "file-changed"
	}
	if !archiveMedia(media) || media.State != models.ArchiveEntityActive {
		ret.Status = "media-unavailable"
	} else if ret.Status == "valid" && check.owners[0].UUID != media.UUID {
		ret.Status = "owner-changed"
	}
	return ret, nil
}

func sourceAlbumCandidateMatchesKind(candidate models.SourceAlbumCandidate, entries []models.SourceAttachmentManifestEntry, attachment string) bool {
	for _, entry := range entries {
		if entry.Attachment.UUID != attachment {
			continue
		}
		if (entry.MediaKind == "image" && candidate.MediaKind != models.ArchiveImage) ||
			(entry.MediaKind == "video" && candidate.MediaKind != models.ArchiveScene) {
			return false
		}
	}
	return true
}

func (s *SourceGalleryStore) PreviewBackfill(ctx context.Context, post, policy string) (*models.SourceAlbumBackfillPreview, error) {
	if !models.ValidSourceAlbumPolicy(policy) {
		return nil, models.ErrSourceAlbumPolicy
	}
	gallery, err := s.Preview(ctx, post)
	if err != nil {
		return nil, err
	}
	ret := &models.SourceAlbumBackfillPreview{PostUUID: gallery.PostUUID, Policy: policy, Gallery: gallery, Matches: []models.SourceAlbumMatch{}}
	if gallery.Action == "create" || gallery.Action == "sync" {
		ret.Matches, err = s.backfillMatches(ctx, gallery.PostUUID, policy)
		if err != nil {
			return nil, err
		}
		proposed := make(map[string]sourceAlbumMediaChoice)
		for _, match := range ret.Matches {
			if match.Status == "matched" {
				proposed[match.AttachmentUUID] = sourceAlbumMediaChoice{AttachmentUUID: match.AttachmentUUID, State: "linked", MediaUUID: sql.NullString{String: match.Candidates[0].MediaUUID, Valid: true}}
			}
		}
		ret.Gallery, err = s.previewWithMediaChoices(ctx, gallery.PostUUID, proposed)
		if err != nil {
			return nil, err
		}
	}
	ret.Signature, err = sourceSignature("stash-source-album-backfill-v1", []any{ret.PostUUID, ret.Policy, ret.Gallery.Signature, ret.Matches})
	return ret, err
}

func (s *SourceGalleryStore) backfillMatches(ctx context.Context, post, policy string) ([]models.SourceAlbumMatch, error) {
	selection, err := (&SourceAttachmentStore{}).Selection(ctx, post)
	if err != nil {
		return nil, err
	}
	if selection == nil || len(selection.Entries) > maxSourceGalleryMembers {
		return nil, models.ErrSourceAlbumLimit
	}
	choices, err := sourceAlbumChoices(ctx, selection)
	if err != nil {
		return nil, err
	}
	rows, err := sourceAlbumEvidenceRows(ctx, post)
	if err != nil {
		return nil, err
	}
	posts, err := sourceAlbumPostIdentifiers(ctx, post)
	if err != nil {
		return nil, err
	}
	requested := make(map[string]bool)
	for _, row := range rows {
		requested[row.MediaUUID] = true
		if row.FileUUID.Valid {
			requested[row.FileUUID.String] = true
		}
	}
	identities, err := sourceGalleryIdentities(ctx, requested)
	if err != nil {
		return nil, err
	}
	ret := []models.SourceAlbumMatch{}
	seen := make(map[string]bool)
	cache := make(map[string]*sourceAlbumProofCheck)
	proofCount := 0
	for _, entry := range selection.Entries {
		a := entry.Attachment
		if seen[a.UUID] {
			continue
		}
		seen[a.UUID] = true
		item := models.SourceAlbumMatch{AttachmentUUID: a.UUID, AttachmentRevision: a.Revision, Reference: a.Reference, Status: "unavailable", Candidates: []models.SourceAlbumCandidate{}}
		if choice, exists := choices[a.UUID]; exists {
			// Even a deliberately undecided review remains a user decision.
			item.Status, item.DecisionUUID = "preserved", choice.DecisionUUID
			ret = append(ret, item)
			continue
		}
		candidates := make(map[string]*models.SourceAlbumCandidate)
		for _, row := range rows {
			basis := sourceAlbumEvidenceBasis(row, a, posts, policy)
			if basis == "" {
				continue
			}
			proofCount++
			if proofCount > maxSourceGalleryMembers {
				return nil, models.ErrSourceAlbumLimit
			}
			media := identities[row.MediaUUID]
			if !archiveMedia(media) {
				return nil, models.ErrSourcePayloadCorrupt
			}
			proof, err := sourceAlbumProof(ctx, row, basis, media, cache, identities)
			if err != nil {
				return nil, err
			}
			candidate := candidates[media.UUID]
			if candidate == nil {
				candidate = &models.SourceAlbumCandidate{MediaUUID: media.UUID, MediaRevision: media.Revision, MediaKind: media.Kind, Proofs: []models.SourceAlbumProof{}}
				candidates[media.UUID] = candidate
			}
			candidate.Proofs = append(candidate.Proofs, *proof)
		}
		for _, candidate := range candidates {
			item.Candidates = append(item.Candidates, *candidate)
		}
		slices.SortFunc(item.Candidates, func(a, b models.SourceAlbumCandidate) int { return cmp.Compare(a.MediaUUID, b.MediaUUID) })
		if len(item.Candidates) > 1 {
			item.Status, item.Reason = "ambiguous", "multiple-media-candidates"
		} else if len(item.Candidates) == 1 {
			item.Status, item.Reason = "review", "no-current-file-proof"
			candidate := item.Candidates[0]
			if !sourceAlbumCandidateMatchesKind(candidate, selection.Entries, a.UUID) {
				item.Reason = "media-kind-conflict"
			} else if slices.ContainsFunc(candidate.Proofs, func(p models.SourceAlbumProof) bool { return p.Status == "valid" }) {
				item.Status, item.Reason = "matched", ""
			}
		}
		ret = append(ret, item)
	}
	return ret, nil
}

func (s *SourceGalleryStore) Backfill(ctx context.Context, post, policy, signature string) (*models.SourceAlbumBackfillResult, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	preview, err := s.PreviewBackfill(ctx, post, policy)
	if err != nil {
		return nil, err
	}
	if signature == "" || signature != preview.Signature || preview.Gallery.Action == "review" {
		return nil, models.ErrSourceGalleryConflict
	}
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrSourceGalleryConflict
		}
		return nil
	})
	result := &models.SourceAlbumBackfillResult{}
	proofs := make(map[string]models.SourceAlbumCandidate)
	store := &SourceAttachmentStore{}
	for _, match := range preview.Matches {
		switch match.Status {
		case "unavailable":
			result.Unavailable++
		case "review", "ambiguous":
			result.Review++
		case "matched":
			candidate := match.Candidates[0]
			for _, proof := range candidate.Proofs {
				if proof.Status != "valid" {
					continue
				}
				details, err := json.Marshal(map[string]string{"policy": policy, "basis": proof.Basis, "source_media_evidence_uuid": proof.EvidenceUUID,
					"source_post_file_evidence_uuid": proof.PostFileUUID, "source_file_match_uuid": proof.MatchUUID})
				if err != nil {
					return nil, err
				}
				id := uuid.NewSHA1(uuid.MustParse(proof.EvidenceUUID), []byte("source-album-match\x00"+policy+"\x00"+match.AttachmentUUID)).String()
				if _, err := store.RecordMediaEvidence(ctx, models.SourceMediaEvidence{UUID: id, PostUUID: preview.PostUUID, AttachmentUUID: match.AttachmentUUID,
					MediaUUID: candidate.MediaUUID, FileUUID: &proof.FileUUID, Basis: "legacy", Details: details}); err != nil {
					return nil, err
				}
				proofs[proof.MatchUUID] = candidate
			}
			attachment, err := store.Find(ctx, match.AttachmentUUID)
			if err != nil {
				return nil, err
			}
			if _, err := store.DecideMedia(ctx, models.AttachmentMediaDecisionInput{AttachmentUUID: match.AttachmentUUID, ExpectedAttachmentRevision: attachment.Revision,
				State: "linked", MediaUUID: candidate.MediaUUID, ExpectedMediaRevision: candidate.MediaRevision, Origin: "migration", Reason: "Reviewed source album matching: " + policy}); err != nil {
				return nil, err
			}
			result.Selected++
		}
	}
	gallery, err := s.Preview(ctx, preview.PostUUID)
	if err != nil {
		return nil, err
	}
	result.Gallery, err = s.Sync(ctx, preview.PostUUID, gallery.Signature)
	if err != nil {
		return nil, err
	}
	final, err := s.Preview(ctx, preview.PostUUID)
	if err != nil {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		for id, candidate := range proofs {
			check, err := checkSourceAlbumFile(ctx, id)
			if err != nil {
				return err
			}
			if check.status != "valid" || check.owners[0].UUID != candidate.MediaUUID {
				return models.ErrSourceGalleryConflict
			}
		}
		current, err := s.Preview(ctx, preview.PostUUID)
		if err != nil {
			return err
		}
		if current.Signature != final.Signature {
			return models.ErrSourceGalleryConflict
		}
		return nil
	})
	complete = true
	return result, nil
}
