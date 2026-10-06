package sqlite

import (
	"context"
	"fmt"
	"slices"

	"github.com/stashapp/stash/pkg/models"
)

// Every branch starts from the selected post. Resolve and deduplicate bounded
// candidates before applying a canonical UUID cursor; paging original UUIDs
// first would lose aliases that sort on the other side of that cursor.
const sourcePostBrowserMediaQuery = `SELECT media_uuid FROM (
SELECT media_uuid FROM (SELECT DISTINCT media_uuid FROM source_media_evidence INDEXED BY source_media_evidence_post
 WHERE post_uuid=? LIMIT ?)
UNION
SELECT media_uuid FROM (SELECT media_uuid FROM post_media_links WHERE post_uuid=? LIMIT ?)
UNION
SELECT media_uuid FROM (SELECT DISTINCT d.media_uuid FROM source_attachments a
 CROSS JOIN attachment_media_links l ON l.attachment_uuid=a.uuid
 CROSS JOIN attachment_media_decisions d ON d.uuid=l.decision_uuid AND d.attachment_uuid=a.uuid
 WHERE a.post_uuid=? AND d.media_uuid IS NOT NULL LIMIT ?)
) LIMIT ?`

func (s *SourcePostMediaStore) MediaForPost(ctx context.Context, postID, after string, limit int) ([]models.SourcePostMediaItem, error) {
	if sourceFileIDs(&postID) != nil || (after != "" && sourceFileIDs(&after) != nil) || limit < 1 || limit > 100 {
		return nil, models.ErrSourcePostMediaInvalid
	}
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, postID)
	if err != nil {
		return nil, err
	}
	if post == nil {
		return nil, models.ErrSourcePostMediaInvalid
	}
	var candidates []string
	bound := maxSourceGalleryMembers + 1
	if err := dbWrapper.Select(ctx, &candidates, sourcePostBrowserMediaQuery, postID, bound, postID, bound, postID, bound, bound); err != nil {
		return nil, err
	}
	if len(candidates) > maxSourceGalleryMembers {
		return nil, fmt.Errorf("%w: more than %d retained media identities for one post", models.ErrSourceAlbumLimit, maxSourceGalleryMembers)
	}
	requested := make(map[string]bool, len(candidates))
	for _, id := range candidates {
		requested[id] = true
	}
	resolved, err := sourceGalleryIdentities(ctx, requested)
	if err != nil {
		return nil, err
	}
	canonical := make(map[string]*models.ArchiveEntity)
	for _, media := range resolved {
		if !archiveMedia(media) {
			return nil, models.ErrSourcePayloadCorrupt
		}
		if media.UUID > after {
			canonical[media.UUID] = media
		}
	}
	ids := make([]string, 0, len(canonical))
	for id := range canonical {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	result := make([]models.SourcePostMediaItem, 0, len(ids))
	for _, id := range ids {
		aliases, err := sourceMediaAliases(ctx, id)
		if err != nil {
			return nil, err
		}
		review, err := sourcePostMediaReview(ctx, post, canonical[id], aliases)
		if err != nil {
			return nil, err
		}
		media, err := sourcePostLibraryItem(ctx, canonical[id])
		if err != nil {
			return nil, err
		}
		result = append(result, models.SourcePostMediaItem{Media: media, Association: review.Association,
			HasRetainedEvidence: review.HasRetainedEvidence, LinkedAttachments: review.LinkedAttachments})
	}
	return result, nil
}

func sourcePostLibraryItem(ctx context.Context, entity *models.ArchiveEntity) (*models.SourcePostLibraryItem, error) {
	result := &models.SourcePostLibraryItem{UUID: entity.UUID, Kind: entity.Kind, State: entity.State,
		Revision: entity.Revision, LocalID: entity.LocalID}
	if entity.State != models.ArchiveEntityActive || entity.LocalID == nil {
		return result, nil
	}
	var table string
	switch entity.Kind {
	case models.ArchiveScene:
		table = "scenes"
	case models.ArchiveImage:
		table = "images"
	case models.ArchiveGallery:
		table = "galleries"
	default:
		return nil, models.ErrSourcePayloadCorrupt
	}
	var title struct {
		Text      string `db:"text"`
		Truncated bool   `db:"truncated"`
	}
	if err := dbWrapper.Get(ctx, &title, "SELECT substr(coalesce(title,''),1,512) AS text,coalesce(length(title)>512,0) AS truncated FROM "+table+" WHERE id=?", *entity.LocalID); err != nil {
		return nil, err
	}
	result.Title, result.TitleTruncated = title.Text, title.Truncated
	return result, nil
}
