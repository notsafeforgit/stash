package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
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
	args := make([]interface{}, 0, len(ids)+1)
	for id := range ids {
		args = append(args, id)
	}
	args = append(args, len(ids)+1)
	var rows []sourceAlbumMediaChoice
	if err := dbWrapper.Select(ctx, &rows, currentAlbumAttachmentChoicesQuery+getInBinding(len(ids))+" LIMIT ?", args...); err != nil {
		return nil, err
	}
	if len(rows) > len(ids) {
		return nil, models.ErrAmbiguousSourceMedia
	}
	for _, row := range rows {
		if _, exists := ret[row.AttachmentUUID]; exists {
			return nil, models.ErrAmbiguousSourceMedia
		}
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
	members, err := sourceThreadMembers(ctx, value)
	if errors.Is(err, models.ErrSourceGalleryConflict) || errors.Is(err, models.ErrSourceAlbumLimit) {
		ret, readErr := s.previewSinglePost(ctx, value, proposed, nil, false)
		if readErr != nil {
			return nil, readErr
		}
		ret.Action = "review"
		return finishSourceGalleryPreview(ret)
	}
	if err != nil {
		return nil, err
	}
	if len(members) > 1 {
		return s.previewThread(ctx, value, proposed, members)
	}
	return s.previewSinglePost(ctx, value, proposed, nil, false)
}

func (s *SourceGalleryStore) previewSinglePost(ctx context.Context, value string, proposed map[string]sourceAlbumMediaChoice, shared *models.ArchiveEntity, forceAlbum bool) (*models.SourceGalleryPreview, error) {
	post, err := currentSourcePost(ctx, value)
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
	ready, err := sourceGalleryPreviewReady(ctx, ret)
	if err != nil {
		return nil, err
	}
	if !ready {
		return finishSourceGalleryPreview(ret)
	}
	if shared != nil && ret.Gallery == nil {
		ret.Gallery = shared
	}
	selection, err := (&SourceAttachmentStore{}).Selection(ctx, post.UUID)
	if err != nil {
		return nil, err
	}
	if selection == nil {
		return finishSourceGalleryPreview(ret)
	}
	owners, err := postGallerySourceOwners(ctx, post.UUID)
	if err != nil {
		return nil, err
	}
	ready, err = sourceGallerySelectPreview(ctx, ret, selection, owners, forceAlbum)
	if err != nil {
		return nil, err
	}
	if !ready {
		return finishSourceGalleryPreview(ret)
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
	var postChoices []sourcePostMediaRow
	if err := dbWrapper.Select(ctx, &postChoices, `SELECT d.* FROM source_post_identities i
JOIN post_media_links l ON l.post_uuid=i.post_uuid JOIN post_media_decisions d ON d.uuid=l.decision_uuid
WHERE i.canonical_uuid=? ORDER BY l.media_uuid,l.post_uuid LIMIT ?`, post.UUID, maxSourceGalleryMembers+1); err != nil {
		return nil, err
	}
	if len(postChoices) > maxSourceGalleryMembers {
		return nil, errors.New("source post exceeds the media association preview limit")
	}
	return sourceGalleryMembershipPreview(ctx, ret, selection, choices, postChoices, owners)
}

func sourceGalleryPreviewReady(ctx context.Context, ret *models.SourceGalleryPreview) (bool, error) {
	var err error
	if ret.Association != nil {
		if ret.Association.State == "disabled" {
			ret.Action = "disabled"
			return false, nil
		}
		ret.Gallery, err = (&ArchiveEntityStore{}).Find(ctx, *ret.Association.GalleryUUID)
		if err != nil {
			return false, err
		}
		if ret.Gallery == nil || ret.Gallery.Kind != models.ArchiveGallery {
			return false, models.ErrSourcePayloadCorrupt
		}
		if ret.Gallery.State == models.ArchiveEntityDeleted {
			ret.Action = "disabled"
			return false, nil
		}
		if ret.Gallery.State != models.ArchiveEntityActive {
			ret.Action = "review"
			return false, nil
		}
		pathless, err := sourceGalleryPathless(ctx, *ret.Gallery.LocalID)
		if err != nil {
			return false, err
		}
		if !pathless {
			ret.Action = "review"
			return false, nil
		}
	}
	return true, nil
}

func sourceGallerySelectPreview(ctx context.Context, ret *models.SourceGalleryPreview, selection *models.AttachmentSelection, owners map[string]bool, forceAlbum bool) (bool, error) {
	ret.SelectionUUID = selection.Decision.UUID
	if selection.Decision.Mode == "disabled" {
		ret.Action = "disabled"
		return false, nil
	}
	if ret.Gallery == nil {
		if !selection.IsAlbum() && !forceAlbum {
			return false, nil
		}
		ret.Action, ret.Title = "create", "Source album"
		if selection.Decision.CaptureUUID == nil {
			return false, models.ErrSourcePayloadCorrupt
		}
		owner, metadata, err := sourceGalleryCaptureMetadata(ctx, *selection.Decision.CaptureUUID)
		if err != nil {
			return false, err
		}
		if !owners[owner] {
			return false, models.ErrSourcePayloadCorrupt
		}
		if metadata.Title != nil && *metadata.Title != "" {
			ret.Title = *metadata.Title
		}
		if metadata.OriginalText != nil {
			ret.Details = *metadata.OriginalText
		}
		if metadata.PublishedAt != nil {
			if date, err := models.ParseDate(*metadata.PublishedAt); err == nil {
				ret.Date = &date
			}
		}
	} else {
		ret.Action = "sync"
	}
	return true, nil
}

const sourceGalleryCaptureMetadataQuery = `SELECT c.post_uuid,r.metadata
FROM source_captures c JOIN source_post_revisions r ON r.post_uuid=c.post_uuid AND r.uuid=c.revision_uuid
WHERE c.uuid=?`

// Album labels need only the retained metadata projection. Do not reconstruct
// source bodies, per-media patches or embedded profiles for a gallery preview.
func sourceGalleryCaptureMetadata(ctx context.Context, capture string) (string, models.SourcePostMetadata, error) {
	var row struct {
		PostUUID string `db:"post_uuid"`
		Metadata string `db:"metadata"`
	}
	var metadata models.SourcePostMetadata
	if err := dbWrapper.Get(ctx, &row, sourceGalleryCaptureMetadataQuery, capture); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", metadata, models.ErrSourcePayloadCorrupt
		}
		return "", metadata, err
	}
	if len(row.Metadata) > 262144 || json.Unmarshal([]byte(row.Metadata), &metadata) != nil {
		return "", metadata, models.ErrSourcePayloadCorrupt
	}
	return row.PostUUID, metadata, nil
}

func sourceGalleryMembershipPreview(ctx context.Context, ret *models.SourceGalleryPreview, selection *models.AttachmentSelection, choices map[string]sourceAlbumMediaChoice, postChoices []sourcePostMediaRow, owners map[string]bool) (*models.SourceGalleryPreview, error) {
	var err error
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
	for _, choice := range postChoices {
		requested[choice.MediaUUID] = true
	}
	resolved, err := sourceGalleryIdentities(ctx, requested)
	if err != nil {
		return nil, err
	}
	policies := sourceGalleryPolicies(heads, resolved)
	postStates := resolvedPostMediaStates(postChoices, resolved)
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
				if postStates[media.UUID] == "unlinked" || postStates[media.UUID] == "conflict" {
					item.Status = "post_" + postStates[media.UUID]
					if postStates[media.UUID] == "conflict" {
						ret.Action = "review"
					}
					ret.Entries = append(ret.Entries, item)
					continue
				}
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
		if policy.source.State == "included" && policy.source.PostUUID.Valid && owners[policy.source.PostUUID.String] {
			ret.Remove = append(ret.Remove, *member.resolve())
		}
	}
	for _, values := range [][]models.ArchiveEntity{ret.Add, ret.Remove} {
		slices.SortFunc(values, func(a, b models.ArchiveEntity) int { return cmp.Compare(a.UUID, b.UUID) })
	}
	return finishSourceGalleryPreview(ret)
}
