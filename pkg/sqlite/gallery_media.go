package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/stashapp/stash/pkg/models"
)

type galleryMediaOrder struct {
	UUID     string `json:"uuid"`
	Post     string `json:"post"`
	Position int    `json:"position"`
}

// Resolve current selections through the same merge/choice rules as source
// albums. Only this gallery's associated posts are read, never capture payloads.
func gallerySourceOrder(ctx context.Context, gallery string) ([]galleryMediaOrder, error) {
	var aliases []string
	if err := dbWrapper.Select(ctx, &aliases, sourceAlbumGalleryAliasesQuery, gallery); err != nil {
		return nil, err
	}
	if len(aliases) > 1024 {
		return nil, models.ErrSourceAlbumLimit
	}
	query, args := sourceAlbumGalleryPostsQuery(aliases, "", 1025)
	// Twitter post IDs provide chronological thread order even when historical
	// timestamps are absent. Other sources use their selected publication time.
	query = `WITH posts AS (` + query + `)
SELECT posts.canonical_uuid FROM posts
LEFT JOIN source_post_threads t ON t.post_uuid=posts.canonical_uuid
LEFT JOIN post_attachment_selections s ON s.post_uuid=posts.canonical_uuid
LEFT JOIN post_attachment_decisions d ON d.uuid=s.decision_uuid
LEFT JOIN source_captures c ON c.uuid=d.capture_uuid
LEFT JOIN source_post_revisions r ON r.uuid=c.revision_uuid
ORDER BY CASE WHEN t.post_id IS NOT NULL THEN 0 ELSE 1 END,
length(t.post_id), t.post_id, julianday(json_extract(r.metadata,'$.published_at')), posts.canonical_uuid`
	var posts []string
	if err := dbWrapper.Select(ctx, &posts, query, args...); err != nil {
		return nil, err
	}
	if len(posts) > 1024 {
		return nil, models.ErrSourceAlbumLimit
	}
	ordered := []galleryMediaOrder{}
	requested := map[string]bool{}
	for _, post := range posts {
		selection, err := (&SourceAttachmentStore{}).Selection(ctx, post)
		if errors.Is(err, models.ErrAttachmentSelectionConflict) {
			// The gallery remains browsable while source ordering needs review.
			continue
		}
		if err != nil {
			return nil, err
		}
		if selection == nil || selection.Decision.Mode == "disabled" {
			continue
		}
		choices, err := sourceAlbumChoices(ctx, selection)
		if errors.Is(err, models.ErrAmbiguousSourceMedia) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range selection.Entries {
			choice := choices[entry.Attachment.UUID]
			if choice.State != "linked" || !choice.MediaUUID.Valid {
				continue
			}
			ordered = append(ordered, galleryMediaOrder{UUID: choice.MediaUUID.String, Post: post, Position: entry.Position})
			requested[choice.MediaUUID.String] = true
		}
		if len(ordered) > maxSourceGalleryMembers {
			return nil, models.ErrSourceAlbumLimit
		}
	}
	identities, err := sourceGalleryIdentities(ctx, requested)
	if err != nil {
		return nil, err
	}
	ret := []galleryMediaOrder{}
	seen := map[string]bool{}
	for _, item := range ordered {
		media := identities[item.UUID]
		if media.State != models.ArchiveEntityActive || seen[media.UUID] {
			continue
		}
		item.UUID = media.UUID
		seen[media.UUID] = true
		ret = append(ret, item)
	}
	return ret, nil
}

const galleryLibraryMediaQuery = `WITH source_order AS MATERIALIZED (
 SELECT CAST(key AS INTEGER) AS ordinal,CAST(json_extract(value,'$.uuid') AS TEXT) AS uuid,
 json_extract(value,'$.post') AS post,json_extract(value,'$.position') AS position
 FROM json_each(?)
), members AS (
 SELECT a.uuid,a.kind,i.id,i.title,f.parent_folder_id AS folder_id,f.basename
 FROM galleries_images gi JOIN images i ON i.id=gi.image_id
 JOIN archive_entities a ON a.image_id=i.id
 LEFT JOIN images_files mf ON mf.image_id=i.id AND mf."primary"=1
 LEFT JOIN files f ON f.id=mf.file_id WHERE gi.gallery_id=?
 UNION ALL
 SELECT a.uuid,a.kind,s.id,s.title,f.parent_folder_id AS folder_id,f.basename
 FROM scenes_galleries sg JOIN scenes s ON s.id=sg.scene_id
 JOIN archive_entities a ON a.scene_id=s.id
 LEFT JOIN scenes_files mf ON mf.scene_id=s.id AND mf."primary"=1
 LEFT JOIN files f ON f.id=mf.file_id WHERE sg.gallery_id=?
)
SELECT m.kind,m.id,o.post,o.position
FROM members m LEFT JOIN folders f ON f.id=m.folder_id
LEFT JOIN source_order o ON o.uuid=m.uuid
ORDER BY o.ordinal IS NULL, o.ordinal,
 (COALESCE(f.path,'') || COALESCE(m.basename,'')) COLLATE NATURAL_CI,
 m.title COLLATE NATURAL_CI,m.kind,m.id LIMIT ? OFFSET ?`

func (s *SourceGalleryStore) LibraryMedia(ctx context.Context, id, offset, limit int) (*models.GalleryMediaReferences, error) {
	if id < 1 || offset < 0 || limit < 1 || limit > 100 {
		return nil, errors.New("invalid gallery media page")
	}
	entity, err := (&ArchiveEntityStore{}).FindByLocalID(ctx, models.ArchiveGallery, id)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, sql.ErrNoRows
	}
	order, err := gallerySourceOrder(ctx, entity.UUID)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(order)
	if err != nil {
		return nil, err
	}
	ret := &models.GalleryMediaReferences{Items: []models.GalleryMediaReference{}}
	if err := dbWrapper.Get(ctx, &ret.Count, `SELECT
 (SELECT count(*) FROM galleries_images WHERE gallery_id=?) +
 (SELECT count(*) FROM scenes_galleries WHERE gallery_id=?)`, id, id); err != nil {
		return nil, err
	}
	ret.Signature, err = sourceSignature("stash-gallery-media-v1", []any{entity.UUID, entity.Revision, ret.Count, order})
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Kind     models.ArchiveEntityKind `db:"kind"`
		ID       int                      `db:"id"`
		Post     *string                  `db:"post"`
		Position *int                     `db:"position"`
	}
	if err := dbWrapper.Select(ctx, &rows, galleryLibraryMediaQuery, string(body), id, id, limit, offset); err != nil {
		return nil, err
	}
	for _, row := range rows {
		ret.Items = append(ret.Items, models.GalleryMediaReference{Kind: row.Kind, LocalID: row.ID, SourcePostUUID: row.Post, SourcePosition: row.Position})
	}
	if next := offset + len(rows); next < ret.Count {
		ret.NextOffset = &next
	}
	return ret, nil
}
