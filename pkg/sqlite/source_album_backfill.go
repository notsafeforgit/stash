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

// Only this identity group's indexed evidence is inspected. A bounded but incomplete
// candidate set must never appear unique, so exceeding the limit is an error.
type sourceAlbumEvidence struct {
	UUID                string         `db:"uuid"`
	AttachmentUUID      sql.NullString `db:"attachment_uuid"`
	AttachmentNamespace sql.NullString `db:"attachment_namespace"`
	AttachmentValue     sql.NullString `db:"attachment_value"`
	MediaUUID           string         `db:"media_uuid"`
	FileUUID            sql.NullString `db:"file_uuid"`
	Basis               string         `db:"basis"`
	PostFileUUID        sql.NullString `db:"post_file_uuid"`
	MatchUUID           sql.NullString `db:"match_uuid"`
	RelativePath        sql.NullString `db:"relative_path"`
	SourceMediaID       sql.NullString `db:"source_media_id"`
	SourceMediaType     sql.NullString `db:"source_media_type"`
}

// Bound each member's ordered range before sorting their union. An identity
// group cannot turn a small preview into a full evidence-table sort.
const sourceAlbumEvidenceQuery = `WITH candidates AS MATERIALIZED (
 SELECT item.value AS uuid FROM source_post_identities i
 CROSS JOIN json_each((SELECT json_group_array(uuid) FROM (
  SELECT m.uuid FROM source_media_evidence m INDEXED BY source_media_evidence_post
  WHERE m.post_uuid=i.post_uuid AND (m.attachment_uuid IS NULL OR NOT EXISTS
   (SELECT 1 FROM attachment_media_links l WHERE l.attachment_uuid=m.attachment_uuid))
  ORDER BY m.uuid LIMIT ?
 ))) item WHERE i.canonical_uuid=?
), selected AS (SELECT uuid FROM candidates ORDER BY uuid LIMIT ?)
SELECT m.uuid,m.attachment_uuid,a.namespace AS attachment_namespace,a.value AS attachment_value,m.media_uuid,m.file_uuid,m.basis,
 p.uuid AS post_file_uuid,f.uuid AS match_uuid,o.relative_path,
 CASE WHEN json_type(p.details,'$.source_media_id')='text' THEN json_extract(p.details,'$.source_media_id') END AS source_media_id,
 json_type(p.details,'$.source_media_id') AS source_media_type
 FROM selected CROSS JOIN source_media_evidence m ON m.uuid=selected.uuid
 LEFT JOIN source_attachments a ON a.uuid=m.attachment_uuid
 LEFT JOIN source_post_file_evidence p ON m.attachment_uuid IS NULL AND m.basis='legacy'
  AND p.uuid=json_extract(m.details,'$.source_post_file_evidence_uuid') AND p.post_uuid=m.post_uuid
  AND p.origin='migration' AND p.basis='catalog-appearance'
 LEFT JOIN source_file_observations o ON o.uuid=p.observation_uuid
 LEFT JOIN source_file_matches f ON f.uuid=json_extract(m.details,'$.source_file_match_uuid') AND f.observation_uuid=p.observation_uuid
 ORDER BY m.uuid`

func sourceAlbumEvidenceRows(ctx context.Context, post string) ([]sourceAlbumEvidence, error) {
	var rows []sourceAlbumEvidence
	err := dbWrapper.Select(ctx, &rows, sourceAlbumEvidenceQuery, maxSourceGalleryMembers+1, post, maxSourceGalleryMembers+1)
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
	err := dbWrapper.Select(ctx, &rows, `SELECT p.namespace,p.value FROM source_post_identities i
CROSS JOIN source_post_identifiers p ON p.post_uuid=i.post_uuid
WHERE i.canonical_uuid=? ORDER BY p.namespace,p.value LIMIT ?`, post, maxSourceGalleryMembers+1)
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
		if row.AttachmentNamespace.String == attachment.Reference.Namespace && row.AttachmentValue.String == attachment.Reference.Value {
			return "attachment-evidence"
		}
		return ""
	}
	if row.Basis != "legacy" || !row.PostFileUUID.Valid || !row.MatchUUID.Valid || !row.FileUUID.Valid {
		return ""
	}
	ref := attachment.Reference
	if policy == models.SourceAlbumTwitterFilenameV1 && ref.Namespace == twitterFilenameNamespace &&
		(!row.SourceMediaType.Valid || row.SourceMediaType.String == "null") {
		if key, _, ok := twitterFilenameSlot(row.RelativePath.String, posts); ok && key == ref.Value {
			return "legacy-twitter-filename"
		}
	}
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
	if policy == models.SourceAlbumTwitterFilenameV1 && gallery.Action != "disabled" && gallery.Action != "review" {
		var review bool
		ret.Recovery, review, err = s.previewFilenameSelection(ctx, gallery.PostUUID)
		if err != nil {
			return nil, err
		}
		if ret.Recovery != nil {
			members, err := sourceThreadMembers(ctx, gallery.PostUUID)
			if err != nil {
				return nil, err
			}
			if len(members) > 1 {
				review = true
				ret.Recovery = nil
			}
		}
		if review {
			ret.Gallery.Action = "review"
			ret.Gallery, err = finishSourceGalleryPreview(ret.Gallery)
			if err != nil {
				return nil, err
			}
		} else if ret.Recovery != nil {
			ret.Gallery, err = s.previewSinglePostSelection(ctx, gallery.PostUUID, nil, nil, false, ret.Recovery)
			if err != nil {
				return nil, err
			}
		}
	}
	if ret.Gallery.Action == "create" || ret.Gallery.Action == "sync" {
		ret.Matches, err = s.backfillMatchesSelection(ctx, gallery.PostUUID, policy, ret.Recovery)
		if err != nil {
			return nil, err
		}
		proposed := make(map[string]sourceAlbumMediaChoice)
		for _, match := range ret.Matches {
			if match.Status == "matched" {
				proposed[match.AttachmentUUID] = sourceAlbumMediaChoice{AttachmentUUID: match.AttachmentUUID, State: "linked", MediaUUID: sql.NullString{String: match.Candidates[0].MediaUUID, Valid: true}}
			}
		}
		if ret.Recovery != nil {
			ret.Gallery, err = s.previewSinglePostSelection(ctx, gallery.PostUUID, proposed, nil, false, ret.Recovery)
		} else {
			ret.Gallery, err = s.previewWithMediaChoices(ctx, gallery.PostUUID, proposed)
		}
		if err != nil {
			return nil, err
		}
	}
	if ret.Recovery != nil && ret.Gallery.Action == "create" && len(ret.Gallery.Add) == 0 {
		// Filename evidence alone must not create an empty gallery when none
		// of its surviving media can currently be associated safely.
		ret.Gallery.Action = "review"
		ret.Gallery, err = finishSourceGalleryPreview(ret.Gallery)
		if err != nil {
			return nil, err
		}
	}
	guard := []any{ret.PostUUID, ret.Policy, ret.Gallery.Signature, ret.Matches}
	if ret.Recovery != nil {
		guard = append(guard, ret.Recovery)
	}
	ret.Signature, err = sourceSignature("stash-source-album-backfill-v1", guard)
	return ret, err
}

func (s *SourceGalleryStore) backfillMatchesSelection(ctx context.Context, post, policy string, selection *models.AttachmentSelection) ([]models.SourceAlbumMatch, error) {
	if selection == nil {
		var err error
		selection, err = (&SourceAttachmentStore{}).Selection(ctx, post)
		if err != nil {
			return nil, err
		}
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
	postStates, err := sourcePostMediaStates(ctx, post)
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
			if state := postStates[candidate.MediaUUID]; state == "unlinked" || state == "conflict" {
				item.Reason = "post-media-" + state
			} else if !sourceAlbumCandidateMatchesKind(candidate, selection.Entries, a.UUID) {
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
	if preview.PostUUID != post || signature == "" || signature != preview.Signature || preview.Gallery.Action == "review" {
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
	if preview.Recovery != nil {
		if err := s.recoverFilenameSelection(ctx, post, preview.Recovery); err != nil {
			return nil, err
		}
	}
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
			attachment, err := store.Find(ctx, match.AttachmentUUID)
			if err != nil {
				return nil, err
			}
			if attachment == nil || attachment.Revision != match.AttachmentRevision {
				return nil, models.ErrSourceAttachmentConflict
			}
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
				if _, err := store.RecordMediaEvidence(ctx, models.SourceMediaEvidence{UUID: id, PostUUID: attachment.PostUUID, AttachmentUUID: match.AttachmentUUID,
					MediaUUID: candidate.MediaUUID, FileUUID: &proof.FileUUID, Basis: "legacy", Details: details}); err != nil {
					return nil, err
				}
				proofs[proof.MatchUUID] = candidate
			}
			attachment, err = store.Find(ctx, match.AttachmentUUID)
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
