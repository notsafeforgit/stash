package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

type sourceMediaEvidenceRow struct {
	UUID           string         `db:"uuid"`
	PostUUID       string         `db:"post_uuid"`
	AttachmentUUID sql.NullString `db:"attachment_uuid"`
	CaptureUUID    sql.NullString `db:"capture_uuid"`
	ManifestUUID   sql.NullString `db:"manifest_uuid"`
	Position       sql.NullInt64  `db:"position"`
	MediaUUID      string         `db:"media_uuid"`
	FileUUID       sql.NullString `db:"file_uuid"`
	Basis          string         `db:"basis"`
	Details        string         `db:"details"`
	CreatedAt      Timestamp      `db:"created_at"`
}

func (s *SourceAttachmentStore) InCapture(ctx context.Context, capture, attachment string) (bool, error) {
	for _, value := range []*string{&capture, &attachment} {
		id, err := archiveUUID(*value)
		if err != nil {
			return false, err
		}
		*value = id
	}
	var found bool
	err := dbWrapper.Get(ctx, &found, `SELECT EXISTS(SELECT 1 FROM source_capture_attachment_manifests c
JOIN source_attachment_entries e ON e.manifest_uuid=c.manifest_uuid
WHERE c.capture_uuid=? AND e.attachment_uuid=?)`, capture, attachment)
	return found, err
}

func (r sourceMediaEvidenceRow) resolve() *models.SourceMediaEvidence {
	ret := &models.SourceMediaEvidence{UUID: r.UUID, PostUUID: r.PostUUID, AttachmentUUID: r.AttachmentUUID.String, CaptureUUID: r.CaptureUUID.String,
		MediaUUID: r.MediaUUID, Basis: r.Basis, Details: json.RawMessage(r.Details), CreatedAt: r.CreatedAt.Timestamp}
	if r.FileUUID.Valid {
		ret.FileUUID = &r.FileUUID.String
	}
	return ret
}

func findSourceMediaEvidence(ctx context.Context, id string) (*models.SourceMediaEvidence, error) {
	var row sourceMediaEvidenceRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM source_media_evidence WHERE uuid = ?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func sameArchiveTarget(ctx context.Context, one, two *string) (bool, error) {
	if one == nil || two == nil {
		return one == nil && two == nil, nil
	}
	if *one == *two {
		return true, nil
	}
	store := &ArchiveEntityStore{}
	a, err := store.Resolve(ctx, *one)
	if err != nil {
		return false, err
	}
	b, err := store.Resolve(ctx, *two)
	if err != nil {
		return false, err
	}
	return a != nil && b != nil && a.UUID == b.UUID, nil
}

func archiveMedia(entity *models.ArchiveEntity) bool {
	return entity != nil && (entity.Kind == models.ArchiveScene || entity.Kind == models.ArchiveImage)
}

func fileBelongsToArchiveMedia(ctx context.Context, media, file *models.ArchiveEntity) (bool, error) {
	if !archiveMedia(media) || media.State != models.ArchiveEntityActive || file == nil || file.Kind != models.ArchiveFile || file.State != models.ArchiveEntityActive {
		return false, nil
	}
	table, column := "scenes_files", "scene_id"
	if media.Kind == models.ArchiveImage {
		table, column = "images_files", "image_id"
	}
	var exists bool
	err := dbWrapper.Get(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM "+table+" WHERE "+column+" = ? AND file_id = ?)", *media.LocalID, *file.LocalID)
	return exists, err
}

func (s *SourceAttachmentStore) RecordMediaEvidence(ctx context.Context, input models.SourceMediaEvidence) (*models.SourceMediaEvidence, error) {
	for _, field := range []*string{&input.UUID, &input.PostUUID, &input.MediaUUID} {
		id, err := archiveUUID(*field)
		if err != nil {
			return nil, err
		}
		*field = id
	}
	for _, field := range []*string{&input.AttachmentUUID, &input.CaptureUUID} {
		if *field == "" {
			continue
		}
		id, err := archiveUUID(*field)
		if err != nil {
			return nil, err
		}
		*field = id
	}
	if input.FileUUID != nil {
		id, err := archiveUUID(*input.FileUUID)
		if err != nil {
			return nil, err
		}
		input.FileUUID = &id
	}
	if input.Basis != "observed-file" && input.Basis != "verified-bytes" && input.Basis != "review" && input.Basis != "legacy" {
		return nil, errors.New("invalid source media evidence basis")
	}
	fileProof := input.Basis == "observed-file" || input.Basis == "verified-bytes"
	if fileProof && (input.AttachmentUUID == "" || input.CaptureUUID == "" || input.FileUUID == nil) {
		return nil, errors.New("observed media evidence requires an attachment, capture and file")
	}
	details, err := accountEvidenceJSON(input.Details)
	if err != nil {
		return nil, err
	}
	if existing, err := findSourceMediaEvidence(ctx, input.UUID); err != nil {
		return nil, err
	} else if existing != nil {
		mediaEqual, err := sameArchiveTarget(ctx, &existing.MediaUUID, &input.MediaUUID)
		if err != nil {
			return nil, err
		}
		fileEqual, err := sameArchiveTarget(ctx, existing.FileUUID, input.FileUUID)
		if err != nil {
			return nil, err
		}
		if existing.PostUUID != input.PostUUID || existing.AttachmentUUID != input.AttachmentUUID || existing.CaptureUUID != input.CaptureUUID || existing.Basis != input.Basis || string(existing.Details) != details || !mediaEqual || !fileEqual {
			return nil, models.ErrSourceMediaEvidenceReplay
		}
		return existing, nil
	}
	manifest, position, err := s.mediaEvidenceScope(ctx, input)
	if err != nil {
		return nil, err
	}
	identities := &ArchiveEntityStore{}
	media, err := identities.Resolve(ctx, input.MediaUUID)
	if err != nil {
		return nil, err
	}
	if !archiveMedia(media) {
		return nil, errors.New("media evidence requires a scene or image identity")
	}
	var file *models.ArchiveEntity
	if input.FileUUID != nil {
		file, err = identities.Resolve(ctx, *input.FileUUID)
		if err != nil {
			return nil, err
		}
		if file == nil || file.Kind != models.ArchiveFile {
			return nil, errors.New("media evidence requires a file identity")
		}
	}
	if fileProof {
		linked, err := fileBelongsToArchiveMedia(ctx, media, file)
		if err != nil {
			return nil, err
		}
		if !linked {
			return nil, errors.New("observed media evidence requires a current file association")
		}
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_media_evidence(uuid, post_uuid, attachment_uuid, capture_uuid, manifest_uuid, position, media_uuid, file_uuid, basis, details)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, input.UUID, input.PostUUID,
		sql.NullString{String: input.AttachmentUUID, Valid: input.AttachmentUUID != ""},
		sql.NullString{String: input.CaptureUUID, Valid: input.CaptureUUID != ""},
		manifest, position, input.MediaUUID, input.FileUUID, input.Basis, details); err != nil {
		return nil, err
	}
	return findSourceMediaEvidence(ctx, input.UUID)
}

// Validate only the evidence actually supplied. Legacy appearances do not
// establish a capture or source order just because their post has captures.
func (s *SourceAttachmentStore) mediaEvidenceScope(ctx context.Context, input models.SourceMediaEvidence) (*string, *int, error) {
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, input.PostUUID)
	if err != nil {
		return nil, nil, err
	}
	if post == nil || post.State != "active" {
		return nil, nil, models.ErrSourcePostForgotten
	}
	if input.AttachmentUUID != "" {
		attachment, err := s.Find(ctx, input.AttachmentUUID)
		if err != nil {
			return nil, nil, err
		}
		if attachment == nil || attachment.PostUUID != post.UUID {
			return nil, nil, models.ErrSourceAttachmentConflict
		}
	}
	if input.CaptureUUID != "" {
		var capturePost string
		if err := dbWrapper.Get(ctx, &capturePost, "SELECT post_uuid FROM source_captures WHERE uuid=?", input.CaptureUUID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, err
		}
		if capturePost != post.UUID {
			return nil, nil, errors.New("media evidence capture belongs to another post or is missing")
		}
	}
	if input.AttachmentUUID == "" || input.CaptureUUID == "" {
		return nil, nil, nil
	}
	manifest, err := s.ManifestForCapture(ctx, input.CaptureUUID)
	if err != nil {
		return nil, nil, err
	}
	if manifest == nil || manifest.PostUUID != post.UUID {
		return nil, nil, errors.New("media evidence requires a capture containing this attachment")
	}
	var position int
	if err := dbWrapper.Get(ctx, &position, `SELECT position FROM source_attachment_entries
WHERE manifest_uuid=? AND attachment_uuid=? ORDER BY position LIMIT 1`, manifest.UUID, input.AttachmentUUID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, errors.New("media evidence attachment is absent from the capture manifest")
		}
		return nil, nil, err
	}
	return &manifest.UUID, &position, nil
}

func (s *SourceAttachmentStore) MediaEvidence(ctx context.Context, value, after string, limit int) ([]models.SourceMediaEvidence, error) {
	return sourceMediaEvidencePage(ctx, "attachment_uuid", value, after, limit)
}

// PostMediaEvidence includes both post-only and attachment-specific evidence.
func (s *SourceAttachmentStore) PostMediaEvidence(ctx context.Context, value, after string, limit int) ([]models.SourceMediaEvidence, error) {
	return sourceMediaEvidencePage(ctx, "post_uuid", value, after, limit)
}

func sourceMediaEvidencePage(ctx context.Context, column, value, after string, limit int) ([]models.SourceMediaEvidence, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	if after != "" {
		after, err = archiveUUID(after)
		if err != nil {
			return nil, err
		}
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	var rows []sourceMediaEvidenceRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM source_media_evidence WHERE "+column+" = ? AND uuid > ? ORDER BY uuid LIMIT ?", id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.SourceMediaEvidence, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, *row.resolve())
	}
	return ret, nil
}

type attachmentMediaDecisionRow struct {
	UUID           string         `db:"uuid"`
	AttachmentUUID string         `db:"attachment_uuid"`
	Revision       int            `db:"revision"`
	State          string         `db:"state"`
	MediaUUID      sql.NullString `db:"media_uuid"`
	Origin         string         `db:"origin"`
	Reason         string         `db:"reason"`
	CreatedAt      Timestamp      `db:"created_at"`
}

func (r attachmentMediaDecisionRow) resolve() *models.AttachmentMediaDecision {
	ret := &models.AttachmentMediaDecision{UUID: r.UUID, AttachmentUUID: r.AttachmentUUID, Revision: r.Revision,
		State: r.State, Origin: r.Origin, Reason: r.Reason, CreatedAt: r.CreatedAt.Timestamp}
	if r.MediaUUID.Valid {
		ret.MediaUUID = &r.MediaUUID.String
	}
	return ret
}

func (s *SourceAttachmentStore) MediaDecision(ctx context.Context, value string) (*models.AttachmentMediaDecision, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	var rows []attachmentMediaDecisionRow
	if err := dbWrapper.Select(ctx, &rows, currentAttachmentChoicesQuery, id); err != nil {
		return nil, err
	}
	if len(rows) > 1 {
		return nil, models.ErrAmbiguousSourceMedia
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].resolve(), nil
}

func (s *SourceAttachmentStore) mediaDecisionForOriginal(ctx context.Context, value string) (*models.AttachmentMediaDecision, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	var row attachmentMediaDecisionRow
	if err := dbWrapper.Get(ctx, &row, `SELECT d.* FROM attachment_media_links l
JOIN attachment_media_decisions d ON d.attachment_uuid = l.attachment_uuid AND d.uuid = l.decision_uuid WHERE l.attachment_uuid = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func (s *SourceAttachmentStore) MediaDecisionHistory(ctx context.Context, value string, after, limit int) ([]models.AttachmentMediaDecision, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, errors.New("invalid attachment media decision cursor")
	}
	var rows []attachmentMediaDecisionRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM attachment_media_decisions WHERE attachment_uuid = ? AND revision > ? ORDER BY revision LIMIT ?", id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.AttachmentMediaDecision, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, *row.resolve())
	}
	return ret, nil
}

func uniqueIngestMediaCandidate(ctx context.Context, attachment, target string) error {
	var rows []struct {
		MediaUUID string         `db:"media_uuid"`
		FileUUID  sql.NullString `db:"file_uuid"`
		Basis     string         `db:"basis"`
	}
	// Bound work even for a heavily observed attachment. Beyond this threshold
	// require explicit review; no full-library or unbounded history scan.
	if err := dbWrapper.Select(ctx, &rows, `SELECT e.media_uuid,e.file_uuid,e.basis FROM source_attachments requested
JOIN source_post_identities root ON root.post_uuid=requested.post_uuid
CROSS JOIN source_post_identities member ON member.canonical_uuid=root.canonical_uuid
JOIN source_attachments a ON a.post_uuid=member.post_uuid AND a.namespace=requested.namespace AND a.value=requested.value
JOIN source_media_evidence e ON e.attachment_uuid=a.uuid
WHERE requested.uuid=? LIMIT 1001`, attachment); err != nil {
		return err
	}
	if len(rows) == 0 || len(rows) > 1000 {
		return models.ErrAmbiguousSourceMedia
	}
	identities := &ArchiveEntityStore{}
	verified := false
	for _, row := range rows {
		media, err := identities.Resolve(ctx, row.MediaUUID)
		if err != nil {
			return err
		}
		if !archiveMedia(media) || media.State != models.ArchiveEntityActive || media.UUID != target {
			return models.ErrAmbiguousSourceMedia
		}
		if row.FileUUID.Valid && (row.Basis == "observed-file" || row.Basis == "verified-bytes") {
			file, err := identities.Resolve(ctx, row.FileUUID.String)
			if err != nil {
				return err
			}
			linked, err := fileBelongsToArchiveMedia(ctx, media, file)
			if err != nil {
				return err
			}
			verified = verified || linked
		}
	}
	if !verified {
		return models.ErrAmbiguousSourceMedia
	}
	return nil
}

func (s *SourceAttachmentStore) prepareMediaChoice(ctx context.Context, input models.AttachmentMediaDecisionInput) (*models.SourceAttachment, *models.ArchiveEntity, error) {
	normalized, err := s.normalizeMediaChoice(ctx, input)
	if err != nil {
		return nil, nil, err
	}
	return s.prepareMediaChoiceWithDeleted(ctx, normalized, false)
}

func (s *SourceAttachmentStore) prepareMediaChoiceWithDeleted(ctx context.Context, input models.AttachmentMediaDecisionInput, allowDeleted bool) (*models.SourceAttachment, *models.ArchiveEntity, error) {
	attachment, err := s.Find(ctx, input.AttachmentUUID)
	if err != nil {
		return nil, nil, err
	}
	if attachment == nil || attachment.Revision != input.ExpectedAttachmentRevision {
		return nil, nil, models.ErrSourceAttachmentConflict
	}
	if err := activeAttachmentPost(ctx, attachment); err != nil {
		return nil, nil, err
	}
	if (input.Origin != "review" && input.Origin != "ingest" && input.Origin != "migration") || !validAccountText(input.Reason, 4096, true) {
		return nil, nil, errors.New("invalid attachment media decision origin or reason")
	}
	var media *models.ArchiveEntity
	switch input.State {
	case "linked":
		media, err = (&ArchiveEntityStore{}).Find(ctx, input.MediaUUID)
		if err != nil {
			return nil, nil, err
		}
		validMedia := archiveMedia(media) && (media.State == models.ArchiveEntityActive ||
			(allowDeleted && media.State == models.ArchiveEntityDeleted))
		if !validMedia || media.Revision != input.ExpectedMediaRevision {
			return nil, nil, models.ErrSourceAttachmentConflict
		}
		association, err := (&SourcePostMediaStore{}).Association(ctx, attachment.PostUUID, media.UUID)
		if err != nil {
			return nil, nil, err
		}
		if association.Suppressed() {
			return nil, nil, models.ErrSourcePostMediaConflict
		}
	case "unlinked", "undecided":
		if input.MediaUUID != "" || input.ExpectedMediaRevision != 0 {
			return nil, nil, errors.New("an unlinked or undecided attachment cannot select media")
		}
	default:
		return nil, nil, errors.New("invalid attachment media choice")
	}
	if input.Origin == "ingest" {
		current, err := s.MediaDecision(ctx, attachment.UUID)
		if err != nil {
			return nil, nil, err
		}
		if media == nil || (current != nil && current.State != "undecided") {
			return nil, nil, models.ErrSourceAttachmentConflict
		}
		if err := uniqueIngestMediaCandidate(ctx, attachment.UUID, media.UUID); err != nil {
			return nil, nil, err
		}
	}
	return attachment, media, nil
}

func (s *SourceAttachmentStore) DecideMedia(ctx context.Context, input models.AttachmentMediaDecisionInput) (*models.AttachmentMediaDecision, error) {
	normalized, err := s.normalizeMediaChoice(ctx, input)
	if err != nil {
		return nil, err
	}
	return s.decideMedia(ctx, normalized, false)
}

func (s *SourceAttachmentStore) decideMedia(ctx context.Context, input models.AttachmentMediaDecisionInput, allowDeleted bool) (*models.AttachmentMediaDecision, error) {
	attachment, media, err := s.prepareMediaChoiceWithDeleted(ctx, input, allowDeleted)
	if err != nil {
		return nil, err
	}
	var mediaUUID *string
	if media != nil {
		mediaUUID = &media.UUID
	}

	result, err := dbWrapper.Exec(ctx, "UPDATE source_attachments SET revision = revision + 1 WHERE uuid = ? AND revision = ?", attachment.UUID, attachment.Revision)
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, models.ErrSourceAttachmentConflict
	}
	if _, err := dbWrapper.Exec(ctx, `UPDATE source_posts SET revision=revision+1 WHERE uuid=?
OR uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=?)`, attachment.PostUUID, attachment.PostUUID); err != nil {
		return nil, err
	}
	id := uuid.NewString()
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO attachment_media_decisions(uuid, attachment_uuid, revision, state, media_uuid, origin, reason)
VALUES (?, ?, ?, ?, ?, ?, ?)`, id, attachment.UUID, attachment.Revision+1, input.State, mediaUUID, input.Origin, input.Reason); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO attachment_media_links(attachment_uuid, decision_uuid) VALUES (?, ?)
ON CONFLICT(attachment_uuid) DO UPDATE SET decision_uuid = excluded.decision_uuid`, attachment.UUID, id); err != nil {
		return nil, err
	}
	return s.mediaDecisionForOriginal(ctx, attachment.UUID)
}
