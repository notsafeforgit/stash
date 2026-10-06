package sqlite

import (
	"context"
	"slices"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// AssociationView is shared by standalone post and ordered album inspection.
// Deleted/redirected gallery identities remain visible without recreating them.
func (s *SourceGalleryStore) AssociationView(ctx context.Context, post string) (*models.SourcePostAlbum, error) {
	association, err := s.Association(ctx, post)
	if err != nil || association == nil {
		return nil, err
	}
	ret := &models.SourcePostAlbum{PostUUID: post, DecisionUUID: association.UUID, Revision: association.Revision,
		State: association.State, GalleryUUID: association.GalleryUUID, SelectionUUID: association.SelectionUUID,
		Origin: association.Origin, Reason: association.Reason, CreatedAt: association.CreatedAt}
	if association.GalleryUUID == nil {
		return ret, nil
	}
	entity, err := (&ArchiveEntityStore{}).Resolve(ctx, *association.GalleryUUID)
	if err != nil {
		return nil, err
	}
	if entity == nil || entity.Kind != models.ArchiveGallery {
		return nil, models.ErrSourcePayloadCorrupt
	}
	ret.Gallery, err = sourcePostLibraryItem(ctx, entity)
	return ret, err
}

// Missing ranges are compressed, including a potentially very large declared
// tail. Paging never fabricates a separate million-row attachment collection.
func sourceAlbumSlots(selection *models.AttachmentSelection) []models.SourceAlbumSlot {
	ret := []models.SourceAlbumSlot{}
	next := 0
	gap := func(through int) {
		if through >= next {
			ret = append(ret, models.SourceAlbumSlot{Position: next, Through: through, MediaKind: "unknown", SelectionState: "unknown", GalleryMembership: "no_gallery"})
		}
	}
	for _, entry := range selection.Entries {
		gap(entry.Position - 1)
		ret = append(ret, models.SourceAlbumSlot{Position: entry.Position, Through: entry.Position, MediaKind: entry.MediaKind,
			SelectionState: "unselected", GalleryMembership: "no_gallery", Attachment: &models.SourceAlbumAttachment{
				UUID: entry.Attachment.UUID, Revision: entry.Attachment.Revision, Reference: models.SourcePostIdentifierSummary(entry.Attachment.Reference)}})
		next = entry.Position + 1
	}
	if selection.ExpectedCount != nil {
		gap(*selection.ExpectedCount - 1)
	}
	return ret
}

type sourceAlbumReadState struct {
	choices    map[string]sourceAlbumMediaChoice
	identities map[string]*models.ArchiveEntity
	postStates map[string]string
	policies   map[string]sourceGalleryPolicy
	members    map[string]bool
}

func sourceAlbumState(ctx context.Context, post string, selection *models.AttachmentSelection, album *models.SourcePostAlbum) (*sourceAlbumReadState, error) {
	choices, err := sourceAlbumChoices(ctx, selection)
	if err != nil {
		return nil, err
	}
	requested := make(map[string]bool)
	for _, choice := range choices {
		if choice.MediaUUID.Valid {
			requested[choice.MediaUUID.String] = true
		}
	}
	var heads []galleryMembershipEventRow
	members := make(map[string]bool)
	if album != nil && album.Gallery != nil && album.Gallery.State == models.ArchiveEntityActive {
		rows, err := sourceGalleryMembers(ctx, *album.Gallery.LocalID)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			members[row.UUID] = true
		}
		heads, err = sourceGalleryMembershipHeads(ctx, album.Gallery.UUID)
		if err != nil {
			return nil, err
		}
		for _, head := range heads {
			requested[head.MediaUUID] = true
		}
	}
	resolved, err := sourceGalleryIdentities(ctx, requested)
	if err != nil {
		return nil, err
	}
	postStates, err := sourcePostMediaStates(ctx, post)
	if err != nil {
		return nil, err
	}
	return &sourceAlbumReadState{choices: choices, identities: resolved, postStates: postStates,
		policies: sourceGalleryPolicies(heads, resolved), members: members}, nil
}

func (state *sourceAlbumReadState) fill(slot *models.SourceAlbumSlot, hasGallery bool) error {
	if slot.Attachment == nil {
		return nil
	}
	if hasGallery {
		slot.GalleryMembership = "absent"
	}
	choice, exists := state.choices[slot.Attachment.UUID]
	if !exists {
		return nil
	}
	slot.SelectionState, slot.DecisionUUID = choice.State, &choice.DecisionUUID
	if choice.State != "linked" {
		return nil
	}
	media := state.identities[choice.MediaUUID.String]
	if !archiveMedia(media) {
		return models.ErrSourcePayloadCorrupt
	}
	slot.Media = &models.SourcePostLibraryItem{UUID: media.UUID, Kind: media.Kind, State: media.State, Revision: media.Revision, LocalID: media.LocalID}
	slot.PostLinkState = state.postStates[media.UUID]
	if slot.PostLinkState == "" {
		slot.PostLinkState = "undecided"
	}
	if state.members[media.UUID] {
		slot.GalleryMembership = "included"
	} else if policy := state.policies[media.UUID]; policy.library != nil && policy.library.State == "excluded" {
		slot.GalleryMembership = "excluded"
	}
	return nil
}

func sourceAlbumRegisteredFiles(ctx context.Context, media *models.SourcePostLibraryItem) (int, error) {
	if media.State != models.ArchiveEntityActive || media.LocalID == nil {
		return 0, nil
	}
	table, column := "images_files", "image_id"
	if media.Kind == models.ArchiveScene {
		table, column = "scenes_files", "scene_id"
	}
	var count int
	err := dbWrapper.Get(ctx, &count, "SELECT count(*) FROM "+table+" WHERE "+column+"=?", *media.LocalID)
	return count, err
}

// ReadAlbum uses the selected immutable list directly, even when gallery sync
// is disabled, the gallery is deleted or the post is forgotten. It never runs
// sync/backfill and never reconstructs source/profile payloads.
func (s *SourceGalleryStore) ReadAlbum(ctx context.Context, id string, after, limit int) (*models.SourceAlbumPage, error) {
	if !validSourceRunUUID(id) || after < -1 || after >= archive.MaxManifestPositions || limit < 1 || limit > 100 {
		return nil, models.ErrSourcePostBrowseInvalid
	}
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, id)
	if err != nil || post == nil {
		return nil, err
	}
	ret := &models.SourceAlbumPage{PostUUID: post.UUID, PostRevision: post.Revision, PostState: post.State, Slots: []models.SourceAlbumSlot{}}
	ret.Album, err = s.AssociationView(ctx, id)
	if err != nil {
		return nil, err
	}
	selection, err := (&SourceAttachmentStore{}).Selection(ctx, id)
	if err != nil {
		return nil, err
	}
	var all []models.SourceAlbumSlot
	var state *sourceAlbumReadState
	if selection != nil {
		ret.Selection = &models.SourceAlbumSelection{UUID: selection.Decision.UUID, Revision: selection.Decision.Revision,
			Mode: selection.Decision.Mode, Complete: selection.Complete, DeclaredAlbum: selection.DeclaredAlbum,
			ExpectedCount: selection.ExpectedCount, EntryCount: len(selection.Entries)}
		all = sourceAlbumSlots(selection)
		state, err = sourceAlbumState(ctx, id, selection, ret.Album)
		if err != nil {
			return nil, err
		}
		for i := range all {
			if err := state.fill(&all[i], ret.Album != nil && ret.Album.Gallery != nil); err != nil {
				return nil, err
			}
		}
	}
	ret.Signature, err = sourceSignature("stash-source-album-read-v1", []interface{}{ret.PostUUID, ret.PostRevision, ret.PostState, ret.Selection, ret.Album, all})
	if err != nil {
		return nil, err
	}
	start := 0
	for start < len(all) && all[start].Through <= after {
		start++
	}
	end := min(start+limit, len(all))
	ret.Slots = slices.Clone(all[start:end])
	if ret.Slots == nil {
		ret.Slots = []models.SourceAlbumSlot{}
	}
	if end < len(all) {
		cursor := all[end-1].Through
		ret.NextAfter = &cursor
	}
	// A caller may resume inside a compressed missing range.
	if len(ret.Slots) > 0 && ret.Slots[0].Position <= after {
		ret.Slots[0].Position = after + 1
	}
	items := make(map[string]*models.SourcePostLibraryItem)
	files := make(map[string]int)
	for i := range ret.Slots {
		slot := &ret.Slots[i]
		if slot.Media == nil {
			continue
		}
		id := slot.Media.UUID
		if items[id] == nil {
			items[id], err = sourcePostLibraryItem(ctx, &models.ArchiveEntity{UUID: id, Kind: slot.Media.Kind,
				State: slot.Media.State, Revision: slot.Media.Revision, LocalID: slot.Media.LocalID})
			if err != nil {
				return nil, err
			}
			files[id], err = sourceAlbumRegisteredFiles(ctx, items[id])
			if err != nil {
				return nil, err
			}
		}
		slot.Media, slot.RegisteredFiles = items[id], files[id]
	}
	return ret, nil
}

const sourceAlbumGalleryAliasesQuery = `WITH RECURSIVE identities(uuid) AS (
SELECT ? UNION SELECT e.uuid FROM archive_entities e JOIN identities i ON e.redirect_to=i.uuid LIMIT 1025
) SELECT uuid FROM identities ORDER BY uuid`

func sourceAlbumGalleryPostsQuery(ids []string, after string, limit int) (string, []interface{}) {
	args := make([]interface{}, 0, len(ids)+2)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, after, limit)
	return `SELECT post_uuid FROM post_gallery_links INDEXED BY post_gallery_links_gallery
WHERE gallery_uuid IN ` + getInBinding(len(ids)) + ` AND post_uuid>? ORDER BY post_uuid LIMIT ?`, args
}

func (s *SourceGalleryStore) PostsForGallery(ctx context.Context, id, after string, limit int) (*models.SourceGalleryPosts, error) {
	if !validSourceRunUUID(id) || (after != "" && !validSourceRunUUID(after)) || limit < 1 || limit > 100 {
		return nil, models.ErrSourcePostBrowseInvalid
	}
	entity, err := (&ArchiveEntityStore{}).Resolve(ctx, id)
	if err != nil || entity == nil {
		return nil, err
	}
	if entity.Kind != models.ArchiveGallery {
		return nil, models.ErrSourcePostBrowseInvalid
	}
	item, err := sourcePostLibraryItem(ctx, entity)
	if err != nil {
		return nil, err
	}
	var ids []string
	if err := dbWrapper.Select(ctx, &ids, sourceAlbumGalleryAliasesQuery, entity.UUID); err != nil {
		return nil, err
	}
	if len(ids) > 1024 {
		return nil, models.ErrSourceAlbumLimit
	}
	query, args := sourceAlbumGalleryPostsQuery(ids, after, limit)
	var posts []string
	if err := dbWrapper.Select(ctx, &posts, query, args...); err != nil {
		return nil, err
	}
	ret := &models.SourceGalleryPosts{RequestedUUID: id, Gallery: *item, Posts: []models.SourcePostSummary{}}
	for _, post := range posts {
		summary, err := (&SourceEvidenceStore{}).PostSummary(ctx, post)
		if err != nil {
			return nil, err
		}
		if summary == nil {
			return nil, models.ErrSourcePayloadCorrupt
		}
		ret.Posts = append(ret.Posts, *summary)
	}
	return ret, nil
}
