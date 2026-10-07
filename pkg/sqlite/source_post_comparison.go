package sqlite

import (
	"context"
	"database/sql"
	"slices"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const maxPostComparisonReferences = 512
const maxPostComparisonChoices = 8192

const postComparisonIdentifiersQuery = `SELECT namespace,value FROM source_post_identifiers
WHERE post_uuid=? ORDER BY namespace,value LIMIT ?`
const postComparisonURLsQuery = `SELECT url FROM source_post_urls WHERE post_uuid=? ORDER BY url LIMIT ?`
const postComparisonAttachmentsQuery = `SELECT a.*,d.uuid AS decision_uuid,d.state AS decision_state,d.revision AS decision_revision,
d.media_uuid,d.origin,d.reason,d.created_at
FROM source_attachments a LEFT JOIN attachment_media_links l ON l.attachment_uuid=a.uuid
LEFT JOIN attachment_media_decisions d ON d.uuid=l.decision_uuid
WHERE a.post_uuid=? ORDER BY a.namespace,a.value LIMIT ?`
const postComparisonMediaQuery = `SELECT d.* FROM post_media_links l
JOIN post_media_decisions d ON d.uuid=l.decision_uuid WHERE l.post_uuid=? ORDER BY l.media_uuid LIMIT ?`

// The caller uses one read transaction for both posts. No history/receipt table
// is scanned or rewritten. Limits fail explicitly: a truncated list must never
// hide a conflicting identity or a deliberate unlink.
func (s *SourceEvidenceStore) ComparePosts(ctx context.Context, left, right string) (*models.SourcePostComparison, error) {
	if !validSourceRunUUID(left) || !validSourceRunUUID(right) || left == right {
		return nil, models.ErrSourcePostComparisonInvalid
	}
	ret := &models.SourcePostComparison{SharedURLs: []string{}, Conflicts: []models.SourcePostComparisonConflict{}}
	a, leftSelection, err := s.postComparisonState(ctx, left)
	if err != nil {
		return nil, err
	}
	b, rightSelection, err := s.postComparisonState(ctx, right)
	if err != nil {
		return nil, err
	}
	ret.Left, ret.Right = *a, *b
	for _, value := range a.URLs {
		if slices.Contains(b.URLs, value) {
			ret.SharedURLs = append(ret.SharedURLs, value)
		}
	}
	postComparisonIdentityConflicts(ret)
	if err := postComparisonSelectionConflicts(ret, leftSelection, rightSelection); err != nil {
		return nil, err
	}
	postComparisonChoiceConflicts(ret)
	return ret, nil
}

func (s *SourceEvidenceStore) postComparisonState(ctx context.Context, id string) (*models.SourcePostComparisonState, *models.AttachmentSelection, error) {
	post, err := s.FindPost(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if post == nil {
		return nil, nil, models.ErrSourcePostComparisonMissing
	}
	ret := &models.SourcePostComparisonState{UUID: id, State: post.State, Revision: post.Revision,
		Identifiers: []models.SourcePostIdentifierSummary{}, URLs: []string{},
		Attachments: []models.SourcePostComparisonAttachment{}, MediaChoices: []models.SourcePostComparisonMedia{}}
	if err := dbWrapper.Select(ctx, &ret.Identifiers, postComparisonIdentifiersQuery, id, maxPostComparisonReferences+1); err != nil {
		return nil, nil, err
	}
	if len(ret.Identifiers) > maxPostComparisonReferences {
		return nil, nil, models.ErrSourcePostComparisonLimit
	}
	if err := dbWrapper.Select(ctx, &ret.URLs, postComparisonURLsQuery, id, maxPostComparisonReferences+1); err != nil {
		return nil, nil, err
	}
	if len(ret.URLs) > maxPostComparisonReferences {
		return nil, nil, models.ErrSourcePostComparisonLimit
	}
	ret.LatestCapture, err = sourceReviewLatestCapture(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	ret.Album, err = (&SourceGalleryStore{}).associationViewForOriginal(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	selection, err := (&SourceAttachmentStore{}).selectionForOriginal(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if selection != nil {
		ret.Selection = &models.SourcePostComparisonSelection{DecisionUUID: selection.Decision.UUID,
			Mode: selection.Decision.Mode, Origin: selection.Decision.Origin, CaptureUUID: selection.Decision.CaptureUUID,
			Complete: selection.Complete, DeclaredAlbum: selection.DeclaredAlbum, ExpectedCount: selection.ExpectedCount,
			Entries: []models.SourcePostComparisonSlot{}}
		for _, entry := range selection.Entries {
			ret.Selection.Entries = append(ret.Selection.Entries, models.SourcePostComparisonSlot{
				Position: entry.Position, AttachmentUUID: entry.Attachment.UUID, MediaKind: entry.MediaKind})
		}
	}
	if err := postComparisonChoices(ctx, ret); err != nil {
		return nil, nil, err
	}
	return ret, selection, nil
}

type postComparisonAttachmentRow struct {
	sourceAttachmentRow
	DecisionUUID     sql.NullString `db:"decision_uuid"`
	DecisionState    sql.NullString `db:"decision_state"`
	DecisionRevision sql.NullInt64  `db:"decision_revision"`
	MediaUUID        sql.NullString `db:"media_uuid"`
	Origin           sql.NullString `db:"origin"`
	Reason           sql.NullString `db:"reason"`
	CreatedAt        NullTimestamp  `db:"created_at"`
}

func comparisonEntity(entity *models.ArchiveEntity) models.SourcePostComparisonEntity {
	return models.SourcePostComparisonEntity{UUID: entity.UUID, Kind: entity.Kind, State: entity.State, Revision: entity.Revision, LocalID: entity.LocalID}
}

func postComparisonChoices(ctx context.Context, ret *models.SourcePostComparisonState) error {
	var attachments []postComparisonAttachmentRow
	if err := dbWrapper.Select(ctx, &attachments, postComparisonAttachmentsQuery, ret.UUID, maxPostComparisonChoices+1); err != nil {
		return err
	}
	if len(attachments) > maxPostComparisonChoices {
		return models.ErrSourcePostComparisonLimit
	}
	var media []sourcePostMediaRow
	if err := dbWrapper.Select(ctx, &media, postComparisonMediaQuery, ret.UUID, maxPostComparisonChoices+1); err != nil {
		return err
	}
	if len(media) > maxPostComparisonChoices {
		return models.ErrSourcePostComparisonLimit
	}
	requested := make(map[string]bool)
	for _, row := range attachments {
		if row.MediaUUID.Valid {
			requested[row.MediaUUID.String] = true
		}
	}
	for _, row := range media {
		requested[row.MediaUUID] = true
	}
	resolved, err := sourceGalleryIdentities(ctx, requested)
	if err != nil {
		return err
	}
	for _, row := range attachments {
		entry := models.SourcePostComparisonAttachment{UUID: row.UUID, Namespace: row.Namespace, Value: row.Value, Revision: row.Revision}
		if row.DecisionUUID.Valid {
			decision := models.AttachmentMediaDecision{UUID: row.DecisionUUID.String, AttachmentUUID: row.UUID,
				Revision: int(row.DecisionRevision.Int64), State: row.DecisionState.String, Origin: row.Origin.String, Reason: row.Reason.String, CreatedAt: row.CreatedAt.Timestamp}
			if row.MediaUUID.Valid {
				decision.MediaUUID = &row.MediaUUID.String
			}
			entry.Choice = attachmentMediaDecisionView(&decision)
		}
		if row.MediaUUID.Valid {
			entity := resolved[row.MediaUUID.String]
			if !archiveMedia(entity) {
				return models.ErrSourcePayloadCorrupt
			}
			view := comparisonEntity(entity)
			entry.Media = &view
		}
		ret.Attachments = append(ret.Attachments, entry)
	}
	for _, row := range media {
		entity := resolved[row.MediaUUID]
		if !archiveMedia(entity) {
			return models.ErrSourcePayloadCorrupt
		}
		ret.MediaChoices = append(ret.MediaChoices, models.SourcePostComparisonMedia{Decision: *row.resolve(), Media: comparisonEntity(entity)})
	}
	return nil
}

func postComparisonIdentityConflicts(ret *models.SourcePostComparison) {
	values := make(map[string]map[string]bool)
	namespaces := make(map[string]bool)
	for _, post := range []models.SourcePostComparisonState{ret.Left, ret.Right} {
		if post.State != "active" {
			ret.Conflicts = append(ret.Conflicts, models.SourcePostComparisonConflict{Kind: "post_not_active", PostUUID: post.UUID})
		}
		for _, identifier := range post.Identifiers {
			if strings.HasPrefix(identifier.Namespace, "legacy:") {
				continue
			}
			namespaces[identifier.Namespace] = true
			if values[identifier.Namespace] == nil {
				values[identifier.Namespace] = make(map[string]bool)
			}
			values[identifier.Namespace][identifier.Value] = true
		}
	}
	keys := make([]string, 0, len(namespaces))
	for key := range namespaces {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if len(keys) > 1 {
		ret.Conflicts = append(ret.Conflicts, models.SourcePostComparisonConflict{Kind: "source_namespace", Values: keys})
	}
	for _, key := range keys {
		if len(values[key]) < 2 {
			continue
		}
		items := make([]string, 0, len(values[key]))
		for item := range values[key] {
			items = append(items, item)
		}
		slices.Sort(items)
		ret.Conflicts = append(ret.Conflicts, models.SourcePostComparisonConflict{Kind: "source_identifier", Namespace: key, Values: items})
	}
}

func postComparisonSelectionConflicts(ret *models.SourcePostComparison, left, right *models.AttachmentSelection) error {
	if left == nil || right == nil {
		return nil
	}
	if left.Decision.Mode != right.Decision.Mode {
		ret.Conflicts = append(ret.Conflicts, models.SourcePostComparisonConflict{Kind: "source_list_mode"})
	}
	if left.Decision.Mode == "disabled" || right.Decision.Mode == "disabled" {
		return nil
	}
	inputs := make([]models.SourceAttachmentManifestInput, 0, 2)
	for _, selection := range []*models.AttachmentSelection{left, right} {
		input := models.SourceAttachmentManifestInput{Complete: selection.Complete, DeclaredAlbum: selection.DeclaredAlbum, ExpectedCount: selection.ExpectedCount}
		for _, entry := range selection.Entries {
			input.Entries = append(input.Entries, models.SourceAttachmentEntry{Position: entry.Position, MediaKind: entry.MediaKind, Reference: entry.Attachment.Reference})
		}
		inputs = append(inputs, input)
	}
	merged, err := archive.MergeAttachmentManifests(inputs)
	if err != nil {
		return err
	}
	for _, conflict := range merged.Conflicts {
		ret.Conflicts = append(ret.Conflicts, models.SourcePostComparisonConflict{Kind: "source_list_" + strings.ReplaceAll(conflict.Kind, "-", "_"), Position: conflict.Position})
	}
	return nil
}

func postComparisonChoiceConflicts(ret *models.SourcePostComparison) {
	left, right := ret.Left.Album, ret.Right.Album
	if left != nil && right != nil {
		same := left.State == right.State
		if left.Gallery != nil && right.Gallery != nil {
			same = same && left.Gallery.UUID == right.Gallery.UUID
		} else {
			same = same && left.Gallery == nil && right.Gallery == nil
		}
		if !same {
			ret.Conflicts = append(ret.Conflicts, models.SourcePostComparisonConflict{Kind: "gallery_choice"})
		}
	}
	states := make(map[string]map[string]bool)
	for _, post := range []models.SourcePostComparisonState{ret.Left, ret.Right} {
		if post.Album != nil && post.Album.Gallery != nil && post.Album.Gallery.State != models.ArchiveEntityActive {
			ret.Conflicts = append(ret.Conflicts, models.SourcePostComparisonConflict{Kind: "gallery_unavailable", PostUUID: post.UUID, MediaUUID: post.Album.Gallery.UUID})
		}
		for _, choice := range post.MediaChoices {
			id := choice.Media.UUID
			if states[id] == nil {
				states[id] = make(map[string]bool)
			}
			states[id][choice.Decision.State] = true
		}
	}
	ids := make([]string, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if len(states[id]) > 1 {
			ret.Conflicts = append(ret.Conflicts, models.SourcePostComparisonConflict{Kind: "media_choice", MediaUUID: id})
		}
	}
	attachments := make(map[models.SourcePostIdentifier]models.SourcePostComparisonAttachment)
	for _, a := range ret.Left.Attachments {
		attachments[models.SourcePostIdentifier{Namespace: a.Namespace, Value: a.Value}] = a
	}
	for _, b := range ret.Right.Attachments {
		a, found := attachments[models.SourcePostIdentifier{Namespace: b.Namespace, Value: b.Value}]
		if !found || a.Choice == nil || b.Choice == nil {
			continue
		}
		same := a.Choice.State == b.Choice.State
		if a.Media != nil && b.Media != nil {
			same = same && a.Media.UUID == b.Media.UUID
		} else {
			same = same && a.Media == nil && b.Media == nil
		}
		if !same {
			ret.Conflicts = append(ret.Conflicts, models.SourcePostComparisonConflict{Kind: "attachment_choice", Namespace: b.Namespace, Values: []string{b.Value}})
		}
	}
}
