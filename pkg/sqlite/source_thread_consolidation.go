package sqlite

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
)

// Thread evidence never merges separate albums automatically. The owner can
// review and combine the source albums that one evidenced self-reply thread
// already holds; the thread's ordinary album planning then completes it.
//
// The album of the earliest post that has one survives, so a thread keeps the
// album its opening post already titles. Albums any post outside the thread
// uses are refused rather than pulling that post into the thread.

const threadSourceGalleriesQuery = `SELECT DISTINCT e.uuid,e.revision,g.id AS local_id,coalesce(g.title,'') AS title
FROM source_post_identities i JOIN post_gallery_links l ON l.post_uuid=i.post_uuid
JOIN archive_entities e ON e.uuid=l.gallery_uuid JOIN galleries g ON g.id=e.gallery_id
WHERE i.canonical_uuid=? AND e.state='active' AND g.origin='source' ORDER BY e.uuid`

func (s *SourceGalleryStore) PreviewThreadConsolidation(ctx context.Context, post string) (*models.GalleryConsolidationPreview, error) {
	current, err := threadConsolidationPost(ctx, post)
	if err != nil {
		return nil, err
	}
	members, err := sourceThreadMembers(ctx, current)
	if err != nil {
		return nil, err
	}
	if len(members) < 2 {
		return nil, models.ErrSourceGalleryConflict
	}
	thread := make(map[string]bool, len(members))
	for _, m := range members {
		thread[m.PostUUID] = true
	}
	var rows []galleryConsolidationRow
	seen := map[string]bool{}
	for _, m := range members {
		var linked []galleryConsolidationRow
		if err := dbWrapper.Select(ctx, &linked, threadSourceGalleriesQuery, m.PostUUID); err != nil {
			return nil, err
		}
		for _, row := range linked {
			if !seen[row.UUID] {
				seen[row.UUID] = true
				rows = append(rows, row)
			}
		}
	}
	for _, row := range rows {
		var users []string
		if err := dbWrapper.Select(ctx, &users, `SELECT DISTINCT i.canonical_uuid FROM post_gallery_links l
JOIN source_post_identities i ON i.post_uuid=l.post_uuid JOIN source_posts p ON p.uuid=i.canonical_uuid
WHERE l.gallery_uuid=? AND p.state='active' LIMIT 1025`, row.UUID); err != nil {
			return nil, err
		}
		for _, user := range users {
			if !thread[user] {
				return nil, models.ErrSourceGalleryConflict
			}
		}
	}
	p, heads, err := galleryConsolidationPreview(ctx, "", rows)
	if err != nil {
		return nil, err
	}
	p.Signature, err = sourceSignature("stash-thread-galleries-v1", []any{p, heads, members})
	return p, err
}

func threadConsolidationPost(ctx context.Context, post string) (string, error) {
	if !validSourceRunUUID(post) {
		return "", models.ErrNotFound
	}
	current, err := currentSourcePost(ctx, post)
	if err != nil {
		return "", err
	}
	if current == nil {
		return "", models.ErrNotFound
	}
	if current.State != "active" {
		return "", models.ErrSourcePostForgotten
	}
	return current.UUID, nil
}

// ConsolidateThread applies a reviewed thread preview: it combines the listed
// albums into the first, then publishes the thread's ordinary album plan.
func (s *SourceGalleryStore) ConsolidateThread(ctx context.Context, post, signature string) (*models.ThreadGalleryConsolidationResult, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	p, err := s.PreviewThreadConsolidation(ctx, post)
	if err != nil {
		return nil, err
	}
	if signature == "" || p.Signature != signature || len(p.Conflicts) > 0 {
		return nil, models.ErrSourceGalleryConflict
	}
	current, err := threadConsolidationPost(ctx, post)
	if err != nil {
		return nil, err
	}
	merged, err := s.consolidate(ctx, p)
	if err != nil {
		return nil, err
	}
	ret := &models.ThreadGalleryConsolidationResult{GalleryConsolidationResult: *merged, ThreadAction: "unchanged",
		ThreadAdded: []string{}, ThreadRemoved: []string{}}
	preview, err := s.Preview(ctx, current)
	if err != nil {
		return nil, err
	}
	ret.ThreadAction = preview.Action
	if preview.Action != "create" && preview.Action != "sync" {
		return ret, nil
	}
	synced, err := s.Sync(ctx, current, preview.Signature)
	if err != nil {
		return nil, err
	}
	ret.GalleryUUID, ret.ThreadAction = synced.GalleryUUID, synced.Action
	ret.ThreadAdded, ret.ThreadRemoved = synced.Added, synced.Removed
	return ret, nil
}
