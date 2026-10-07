package sqlite

import (
	"context"
	"database/sql"

	"github.com/stashapp/stash/pkg/models"
)

const currentAttachmentChoicesQuery = `SELECT d.* FROM source_attachments requested
JOIN source_post_identities root ON root.post_uuid=requested.post_uuid
CROSS JOIN source_post_identities member ON member.canonical_uuid=root.canonical_uuid
JOIN source_attachments a ON a.post_uuid=member.post_uuid AND a.namespace=requested.namespace AND a.value=requested.value
JOIN attachment_media_links l ON l.attachment_uuid=a.uuid
JOIN attachment_media_decisions d ON d.uuid=l.decision_uuid WHERE requested.uuid=? LIMIT 2`

const currentAlbumAttachmentChoicesQuery = `SELECT requested.uuid AS attachment_uuid,d.uuid AS decision_uuid,d.state,d.media_uuid
FROM source_attachments requested JOIN source_post_identities root ON root.post_uuid=requested.post_uuid
CROSS JOIN source_post_identities member ON member.canonical_uuid=root.canonical_uuid
JOIN source_attachments a ON a.post_uuid=member.post_uuid AND a.namespace=requested.namespace AND a.value=requested.value
JOIN attachment_media_links l ON l.attachment_uuid=a.uuid
JOIN attachment_media_decisions d ON d.uuid=l.decision_uuid WHERE requested.uuid IN `

// Review requests use the owner returned by the current-choice context. Ingest
// retains its original attachment evidence but may follow an explicit undecided
// choice belonging to another member; it must still prove one shared candidate.
func (s *SourceAttachmentStore) normalizeMediaChoice(ctx context.Context, input models.AttachmentMediaDecisionInput) (models.AttachmentMediaDecisionInput, error) {
	original, err := s.Find(ctx, input.AttachmentUUID)
	if err != nil {
		return input, err
	}
	if original == nil || original.Revision != input.ExpectedAttachmentRevision {
		return input, models.ErrSourceAttachmentConflict
	}
	current, err := s.MediaDecision(ctx, original.UUID)
	if err != nil {
		return input, err
	}
	if current == nil || current.AttachmentUUID == original.UUID {
		return input, nil
	}
	if input.Origin != "ingest" || current.State != "undecided" {
		return input, models.ErrSourceAttachmentConflict
	}
	owner, err := s.Find(ctx, current.AttachmentUUID)
	if err != nil {
		return input, err
	}
	if owner == nil {
		return input, models.ErrSourcePayloadCorrupt
	}
	input.AttachmentUUID, input.ExpectedAttachmentRevision = owner.UUID, owner.Revision
	return input, nil
}

const postAttachmentChoicesQuery = `SELECT a.*,d.uuid AS decision_uuid,d.revision AS decision_revision,
d.state AS decision_state,d.media_uuid,d.origin AS decision_origin,d.reason AS decision_reason,d.created_at AS decision_created_at
FROM source_post_identities i JOIN source_attachments a ON a.post_uuid=i.post_uuid
LEFT JOIN attachment_media_links l ON l.attachment_uuid=a.uuid
LEFT JOIN attachment_media_decisions d ON d.uuid=l.decision_uuid
WHERE i.canonical_uuid=? AND a.namespace=? AND a.value=? ORDER BY a.uuid LIMIT ?`

type postAttachmentChoiceMember struct {
	Attachment models.SourceAttachment
	Decision   *models.AttachmentMediaDecision
	Media      *models.ArchiveEntity
}

type postAttachmentChoiceSnapshot struct {
	Post      models.SourcePostIdentity
	Reference models.SourcePostIdentifier
	Members   []postAttachmentChoiceMember
	Signature string
}

// Qualified references are shared only within one reviewed post identity.
// Originals keep their UUIDs and evidence; no capture or manifest is rewritten.
func inspectPostAttachmentChoices(ctx context.Context, post string, ref models.SourcePostIdentifier) (*postAttachmentChoiceSnapshot, error) {
	if err := validatePostIdentifier(ref); err != nil {
		return nil, err
	}
	identity, err := (&SourceEvidenceStore{}).PostIdentity(ctx, post)
	if err != nil {
		return nil, err
	}
	if identity == nil || identity.CanonicalUUID != post {
		return nil, models.ErrSourceAttachmentConflict
	}
	if identity.State != "active" {
		return nil, models.ErrSourcePostForgotten
	}
	var rows []struct {
		sourceAttachmentRow
		DecisionUUID      sql.NullString `db:"decision_uuid"`
		DecisionRevision  sql.NullInt64  `db:"decision_revision"`
		DecisionState     sql.NullString `db:"decision_state"`
		MediaUUID         sql.NullString `db:"media_uuid"`
		DecisionOrigin    sql.NullString `db:"decision_origin"`
		DecisionReason    sql.NullString `db:"decision_reason"`
		DecisionCreatedAt NullTimestamp  `db:"decision_created_at"`
	}
	if err := dbWrapper.Select(ctx, &rows, postAttachmentChoicesQuery, post, ref.Namespace, ref.Value, maxPostIdentityMembers+1); err != nil {
		return nil, err
	}
	if len(rows) > maxPostIdentityMembers {
		return nil, models.ErrSourcePostIdentityLimit
	}
	if len(rows) == 0 {
		return nil, models.ErrSourceAttachmentConflict
	}
	requested := map[string]bool{}
	for _, row := range rows {
		if row.MediaUUID.Valid {
			requested[row.MediaUUID.String] = true
		}
	}
	resolved, err := sourceGalleryIdentities(ctx, requested)
	if err != nil {
		return nil, err
	}
	ret := &postAttachmentChoiceSnapshot{Post: *identity, Reference: ref, Members: []postAttachmentChoiceMember{}}
	for _, row := range rows {
		member := postAttachmentChoiceMember{Attachment: *row.resolve()}
		if row.DecisionUUID.Valid {
			if !row.DecisionCreatedAt.Valid {
				return nil, models.ErrSourcePayloadCorrupt
			}
			member.Decision = &models.AttachmentMediaDecision{UUID: row.DecisionUUID.String, AttachmentUUID: row.UUID, Revision: int(row.DecisionRevision.Int64),
				State: row.DecisionState.String, Origin: row.DecisionOrigin.String, Reason: row.DecisionReason.String, CreatedAt: *row.DecisionCreatedAt.TimePtr()}
			if row.MediaUUID.Valid {
				id := row.MediaUUID.String
				member.Decision.MediaUUID = &id
				member.Media = resolved[id]
				if !archiveMedia(member.Media) {
					return nil, models.ErrSourcePayloadCorrupt
				}
			}
		}
		ret.Members = append(ret.Members, member)
	}
	ret.Signature, err = sourceSignature("stash-post-attachment-choice-snapshot-v1", ret)
	return ret, err
}

// Called only inside the encompassing reviewed post consolidation. Its saved
// result owns retry; each original attachment decision remains immutable history.
func publishConsolidatedAttachmentMedia(ctx context.Context, post, consolidation, signature string, input models.AttachmentMediaDecisionInput) (*models.AttachmentMediaDecision, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if input.Origin != "review" && input.Origin != "migration" {
		return nil, models.ErrSourceAttachmentConflict
	}
	valid, err := currentPostConsolidation(ctx, post, consolidation)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, models.ErrSourceAttachmentConflict
	}
	store := &SourceAttachmentStore{}
	attachment, err := store.Find(ctx, input.AttachmentUUID)
	if err != nil {
		return nil, err
	}
	if attachment == nil {
		return nil, models.ErrSourceAttachmentConflict
	}
	inScope, err := samePostIdentity(ctx, post, attachment.PostUUID)
	if err != nil {
		return nil, err
	}
	if !inScope {
		return nil, models.ErrSourceAttachmentConflict
	}
	current, err := inspectPostAttachmentChoices(ctx, post, attachment.Reference)
	if err != nil {
		return nil, err
	}
	if signature == "" || current.Signature != signature {
		return nil, models.ErrSourceAttachmentConflict
	}
	allowDeleted := false
	for _, member := range current.Members {
		if member.Decision != nil && member.Decision.State == "linked" && member.Media != nil && member.Media.UUID == input.MediaUUID && member.Media.State == models.ArchiveEntityDeleted {
			allowDeleted = true
		}
	}
	if input.State == "linked" {
		association, err := (&SourcePostMediaStore{}).Association(ctx, post, input.MediaUUID)
		if err != nil {
			return nil, err
		}
		if association.Suppressed() {
			return nil, models.ErrSourcePostMediaConflict
		}
	}
	finish := postConsolidationCommitGuard(ctx, models.ErrSourceAttachmentConflict)
	decision, err := store.decideMedia(ctx, input, allowDeleted)
	if err != nil {
		return nil, err
	}
	args := []interface{}{}
	for _, member := range current.Members {
		if member.Attachment.UUID != attachment.UUID {
			args = append(args, member.Attachment.UUID)
		}
	}
	if len(args) > 0 {
		if _, err := dbWrapper.Exec(ctx, "DELETE FROM attachment_media_links WHERE attachment_uuid IN "+getInBinding(len(args)), args...); err != nil {
			return nil, err
		}
	}
	finish()
	return decision, nil
}
