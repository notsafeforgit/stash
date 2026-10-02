package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/stashapp/stash/pkg/models"
)

const maxSourceGalleryMembers = 8192

// Resolve only requested identities, including their bounded redirect chains.
// Membership previews must not turn each attachment into a separate DB lookup.
func sourceGalleryIdentities(ctx context.Context, requested map[string]bool) (map[string]*models.ArchiveEntity, error) {
	ret := make(map[string]*models.ArchiveEntity)
	if len(requested) == 0 {
		return ret, nil
	}
	if len(requested) > maxSourceGalleryMembers*3 {
		return nil, errors.New("source gallery preview has too many identities")
	}
	args := make([]interface{}, 0, len(requested))
	for id := range requested {
		args = append(args, id)
	}
	var rows []struct {
		archiveEntityRow
		Requested string `db:"requested"`
	}
	query := `WITH RECURSIVE resolved(requested, uuid, depth) AS (
SELECT uuid, uuid, 0 FROM archive_entities WHERE uuid IN ` + getInBinding(len(args)) + `
UNION ALL SELECT r.requested, a.redirect_to, r.depth + 1 FROM resolved r
JOIN archive_entities a ON a.uuid = r.uuid WHERE a.state = 'redirected' AND r.depth < 127
)
SELECT r.requested, a.* FROM resolved r JOIN archive_entities a ON a.uuid = r.uuid
WHERE a.state != 'redirected' OR r.depth = 127`
	if err := dbWrapper.Select(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	if len(rows) != len(requested) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	for _, row := range rows {
		if row.State == models.ArchiveEntityRedirected {
			return nil, errors.New("archive identity redirect chain is too long")
		}
		ret[row.Requested] = row.resolve()
	}
	return ret, nil
}

type sourceGalleryMember struct {
	archiveEntityRow
	Cover bool `db:"cover"`
}

func sourceGalleryMembers(ctx context.Context, id int) ([]sourceGalleryMember, error) {
	var rows []sourceGalleryMember
	if err := dbWrapper.Select(ctx, &rows, `SELECT * FROM (
SELECT a.*, COALESCE(gi.cover, 0) AS cover FROM galleries_images gi JOIN archive_entities a ON a.image_id = gi.image_id WHERE gi.gallery_id = ?
UNION ALL SELECT a.*, 0 AS cover FROM scenes_galleries gs JOIN archive_entities a ON a.scene_id = gs.scene_id WHERE gs.gallery_id = ?
) ORDER BY uuid LIMIT ?`, id, id, maxSourceGalleryMembers+1); err != nil {
		return nil, err
	}
	if len(rows) > maxSourceGalleryMembers {
		return nil, errors.New("source gallery exceeds the membership preview limit")
	}
	return rows, nil
}

func sourceGalleryMembershipHeads(ctx context.Context, id string) ([]galleryMembershipEventRow, error) {
	var rows []galleryMembershipEventRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT e.* FROM gallery_membership_heads h
JOIN gallery_membership_events e ON e.gallery_uuid = h.gallery_uuid AND e.media_uuid = h.media_uuid AND e.uuid = h.event_uuid
WHERE h.gallery_uuid = ? ORDER BY h.media_uuid LIMIT ?`, id, maxSourceGalleryMembers+1); err != nil {
		return nil, err
	}
	if len(rows) > maxSourceGalleryMembers {
		return nil, errors.New("source gallery exceeds the membership decision limit")
	}
	return rows, nil
}

type sourceAlbumMediaChoice struct {
	AttachmentUUID string         `db:"attachment_uuid"`
	DecisionUUID   string         `db:"decision_uuid"`
	State          string         `db:"state"`
	MediaUUID      sql.NullString `db:"media_uuid"`
}

func sourceAlbumChoices(ctx context.Context, selection *models.AttachmentSelection) (map[string]sourceAlbumMediaChoice, error) {
	ret := make(map[string]sourceAlbumMediaChoice)
	ids := make(map[string]bool)
	for _, entry := range selection.Entries {
		ids[entry.Attachment.UUID] = true
	}
	if len(ids) == 0 {
		return ret, nil
	}
	args := make([]interface{}, 0, len(ids))
	for id := range ids {
		args = append(args, id)
	}
	var rows []sourceAlbumMediaChoice
	if err := dbWrapper.Select(ctx, &rows, `SELECT d.attachment_uuid, d.uuid AS decision_uuid, d.state, d.media_uuid FROM attachment_media_links l
JOIN attachment_media_decisions d ON d.attachment_uuid = l.attachment_uuid AND d.uuid = l.decision_uuid
WHERE l.attachment_uuid IN `+getInBinding(len(args)), args...); err != nil {
		return nil, err
	}
	for _, row := range rows {
		ret[row.AttachmentUUID] = row
	}
	return ret, nil
}

type sourceGalleryPolicy struct{ library, source *galleryMembershipEventRow }

func sourceGalleryPolicies(heads []galleryMembershipEventRow, resolved map[string]*models.ArchiveEntity) map[string]sourceGalleryPolicy {
	ret := make(map[string]sourceGalleryPolicy)
	for i := range heads {
		head := &heads[i]
		id := resolved[head.MediaUUID].UUID
		policy := ret[id]
		if head.Origin == "source" {
			if policy.source == nil || policy.source.Sequence < head.Sequence {
				policy.source = head
			}
		} else if policy.library == nil || policy.library.Sequence < head.Sequence {
			policy.library = head
		}
		ret[id] = policy
	}
	return ret
}

func finishSourceGalleryPreview(ret *models.SourceGalleryPreview) (*models.SourceGalleryPreview, error) {
	var gallery, association interface{}
	if ret.Gallery != nil {
		gallery = []interface{}{ret.Gallery.UUID, ret.Gallery.Kind, ret.Gallery.State, ret.Gallery.Revision, ret.Gallery.LocalID}
	}
	if ret.Association != nil {
		association = []interface{}{ret.Association.UUID, ret.Association.Revision, ret.Association.State, ret.Association.GalleryUUID}
	}
	entries := make([][]interface{}, 0, len(ret.Entries))
	for _, entry := range ret.Entries {
		entries = append(entries, []interface{}{entry.Position, entry.AttachmentUUID, entry.MediaUUID, entry.MediaKind, entry.MediaRevision, entry.Status})
	}
	changes := make([][][]interface{}, 0, 2)
	for _, values := range [][]models.ArchiveEntity{ret.Add, ret.Remove} {
		items := make([][]interface{}, 0, len(values))
		for _, value := range values {
			items = append(items, []interface{}{value.UUID, value.Kind, value.Revision, value.LocalID})
		}
		changes = append(changes, items)
	}
	var date interface{}
	if ret.Date != nil {
		date = ret.Date.String()
	}
	signature, err := sourceSignature("stash-source-gallery-preview-v1", []interface{}{ret.PostUUID, ret.PostRevision, ret.SelectionUUID,
		association, gallery, ret.Action, ret.Title, ret.Details, date, entries, changes})
	if err != nil {
		return nil, err
	}
	ret.Signature = signature
	return ret, nil
}

func (s *SourceGalleryStore) Preview(ctx context.Context, value string) (*models.SourceGalleryPreview, error) {
	return s.previewWithMediaChoices(ctx, value, nil)
}

// Proposed choices exist only in a read-only backfill preview. Normal sync
// always reads the actual persisted decisions.
func (s *SourceGalleryStore) previewWithMediaChoices(ctx context.Context, value string, proposed map[string]sourceAlbumMediaChoice) (*models.SourceGalleryPreview, error) {
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, value)
	if err != nil {
		return nil, err
	}
	if post == nil {
		return nil, models.ErrSourceGalleryConflict
	}
	if post.State != "active" {
		return nil, models.ErrSourcePostForgotten
	}
	ret := &models.SourceGalleryPreview{PostUUID: post.UUID, PostRevision: post.Revision, Action: "ineligible", Entries: []models.SourceAlbumEntry{}, Add: []models.ArchiveEntity{}, Remove: []models.ArchiveEntity{}}
	ret.Association, err = s.Association(ctx, post.UUID)
	if err != nil {
		return nil, err
	}
	if ret.Association != nil {
		if ret.Association.State == "disabled" {
			ret.Action = "disabled"
			return finishSourceGalleryPreview(ret)
		}
		ret.Gallery, err = (&ArchiveEntityStore{}).Find(ctx, *ret.Association.GalleryUUID)
		if err != nil {
			return nil, err
		}
		if ret.Gallery == nil || ret.Gallery.Kind != models.ArchiveGallery {
			return nil, models.ErrSourcePayloadCorrupt
		}
		if ret.Gallery.State == models.ArchiveEntityDeleted {
			ret.Action = "disabled"
			return finishSourceGalleryPreview(ret)
		}
		if ret.Gallery.State != models.ArchiveEntityActive {
			ret.Action = "review"
			return finishSourceGalleryPreview(ret)
		}
		pathless, err := sourceGalleryPathless(ctx, *ret.Gallery.LocalID)
		if err != nil {
			return nil, err
		}
		if !pathless {
			ret.Action = "review"
			return finishSourceGalleryPreview(ret)
		}
	}
	selection, err := (&SourceAttachmentStore{}).Selection(ctx, post.UUID)
	if err != nil {
		return nil, err
	}
	if selection == nil {
		return finishSourceGalleryPreview(ret)
	}
	ret.SelectionUUID = selection.Decision.UUID
	if selection.Decision.Mode == "disabled" {
		ret.Action = "disabled"
		return finishSourceGalleryPreview(ret)
	}
	if ret.Gallery == nil {
		if !selection.IsAlbum() {
			return finishSourceGalleryPreview(ret)
		}
		ret.Action, ret.Title = "create", "Source album"
		if selection.Decision.CaptureUUID == nil {
			return nil, models.ErrSourcePayloadCorrupt
		}
		capture, err := (&SourceEvidenceStore{}).FindCapture(ctx, *selection.Decision.CaptureUUID)
		if err != nil {
			return nil, err
		}
		if capture == nil || capture.PostUUID != post.UUID {
			return nil, models.ErrSourcePayloadCorrupt
		}
		if capture.Metadata.Title != nil && *capture.Metadata.Title != "" {
			ret.Title = *capture.Metadata.Title
		}
		if capture.Metadata.OriginalText != nil {
			ret.Details = *capture.Metadata.OriginalText
		}
		if capture.Metadata.PublishedAt != nil {
			if date, err := models.ParseDate(*capture.Metadata.PublishedAt); err == nil {
				ret.Date = &date
			}
		}
	} else {
		ret.Action = "sync"
	}
	choices, err := sourceAlbumChoices(ctx, selection)
	if err != nil {
		return nil, err
	}
	for id, choice := range proposed {
		if _, exists := choices[id]; exists {
			return nil, models.ErrSourceGalleryConflict
		}
		choices[id] = choice
	}
	var members []sourceGalleryMember
	var heads []galleryMembershipEventRow
	if ret.Gallery != nil {
		members, err = sourceGalleryMembers(ctx, *ret.Gallery.LocalID)
		if err != nil {
			return nil, err
		}
		heads, err = sourceGalleryMembershipHeads(ctx, ret.Gallery.UUID)
		if err != nil {
			return nil, err
		}
	}
	requested := make(map[string]bool)
	for _, choice := range choices {
		if choice.MediaUUID.Valid {
			requested[choice.MediaUUID.String] = true
		}
	}
	for _, head := range heads {
		requested[head.MediaUUID] = true
	}
	resolved, err := sourceGalleryIdentities(ctx, requested)
	if err != nil {
		return nil, err
	}
	policies := sourceGalleryPolicies(heads, resolved)
	present, desired := make(map[string]bool), make(map[string]bool)
	for _, member := range members {
		present[member.UUID] = true
	}
	for _, entry := range selection.Entries {
		item := models.SourceAlbumEntry{Position: entry.Position, AttachmentUUID: entry.Attachment.UUID, Status: "unselected"}
		choice := choices[entry.Attachment.UUID]
		if choice.State == "unlinked" {
			item.Status = "unlinked"
		}
		if choice.State == "linked" {
			media := resolved[choice.MediaUUID.String]
			if !archiveMedia(media) {
				return nil, models.ErrSourcePayloadCorrupt
			}
			item.MediaUUID, item.MediaKind, item.MediaRevision = &media.UUID, media.Kind, media.Revision
			item.Status = "deleted"
			if media.State == models.ArchiveEntityActive {
				item.Status = "linked"
				policy := policies[media.UUID]
				if policy.library != nil && policy.library.State == "excluded" {
					item.Status = "excluded"
				}
				if !desired[media.UUID] && !present[media.UUID] && item.Status == "linked" {
					ret.Add = append(ret.Add, *media)
				}
				desired[media.UUID] = true
			}
		}
		ret.Entries = append(ret.Entries, item)
	}
	for _, member := range members {
		policy := policies[member.UUID]
		if desired[member.UUID] || member.Cover || policy.library != nil || policy.source == nil {
			continue
		}
		if policy.source.State == "included" && policy.source.PostUUID.Valid && policy.source.PostUUID.String == post.UUID {
			ret.Remove = append(ret.Remove, *member.resolve())
		}
	}
	for _, values := range [][]models.ArchiveEntity{ret.Add, ret.Remove} {
		slices.SortFunc(values, func(a, b models.ArchiveEntity) int { return cmp.Compare(a.UUID, b.UUID) })
	}
	return finishSourceGalleryPreview(ret)
}
