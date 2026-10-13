package sqlite

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
)

const linkedMediaAlbumReason = "Album from confirmed post media; original attachment order and total count unknown"

func linkedMediaSelection(selection *models.AttachmentSelection) bool {
	return selection != nil && selection.Decision.Reason == linkedMediaAlbumReason &&
		selection.DeclaredAlbum && !selection.Complete && selection.ExpectedCount == nil && len(selection.Entries) == 0
}

// Each candidate's current links are read through the post-leading index. This
// includes posts omitted by ordered-list discovery without scanning capture
// payloads or treating a shared filename/title as a confirmed association.
const linkedMediaAlbumPostsQuery = `SELECT p.uuid AS post_uuid,p.state AS post_state,
 '' AS selection_uuid,'unselected' AS mode
 FROM source_post_identities root INDEXED BY source_post_identities_canonical
 CROSS JOIN source_posts p ON p.uuid=root.post_uuid
 WHERE root.canonical_uuid>? AND root.post_uuid=root.canonical_uuid AND p.state='active'
 AND NOT EXISTS(SELECT 1 FROM post_attachment_selections s WHERE s.post_uuid=p.uuid)
 AND NOT EXISTS(SELECT 1 FROM post_gallery_links g WHERE g.post_uuid=p.uuid)
 AND (SELECT count(*) FROM (SELECT DISTINCT l.media_uuid
  FROM source_post_identities i CROSS JOIN post_media_links l ON l.post_uuid=i.post_uuid
  JOIN post_media_decisions d ON d.uuid=l.decision_uuid
  JOIN archive_entities e ON e.uuid=l.media_uuid
  WHERE i.canonical_uuid=p.uuid AND d.state='linked' AND e.state='active' LIMIT 2))=2
 ORDER BY root.canonical_uuid LIMIT ?`

func (s *SourceGalleryStore) LinkedMediaBackfillPosts(ctx context.Context, after string, limit int) ([]models.SelectedSourcePost, error) {
	if after != "" {
		if id, err := archiveUUID(after); err != nil || id != after {
			return nil, models.ErrSourceAlbumPolicy
		}
	}
	if limit < 1 || limit > 100 {
		return nil, models.ErrSourceAlbumLimit
	}
	var rows []struct {
		PostUUID      string `db:"post_uuid"`
		PostState     string `db:"post_state"`
		SelectionUUID string `db:"selection_uuid"`
		Mode          string `db:"mode"`
	}
	if err := dbWrapper.Select(ctx, &rows, linkedMediaAlbumPostsQuery, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.SelectedSourcePost, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, models.SelectedSourcePost{PostUUID: row.PostUUID, PostState: row.PostState, Mode: row.Mode})
	}
	return ret, nil
}

func (s *SourceGalleryStore) previewLinkedMediaSelection(ctx context.Context, post string) (*models.AttachmentSelection, bool, error) {
	current, err := (&SourceAttachmentStore{}).Selection(ctx, post)
	if err != nil || current != nil {
		return nil, false, err
	}
	var captured, rejected bool
	if err := dbWrapper.Get(ctx, &captured, `SELECT EXISTS(SELECT 1 FROM source_post_identities i
 JOIN source_capture_attachment_manifests m ON m.post_uuid=i.post_uuid WHERE i.canonical_uuid=?)`, post); err != nil {
		return nil, false, err
	}
	if err := dbWrapper.Get(ctx, &rejected, `SELECT EXISTS(SELECT 1 FROM source_post_identities i
 JOIN source_attachments a ON a.post_uuid=i.post_uuid JOIN attachment_media_links l ON l.attachment_uuid=a.uuid
 JOIN attachment_media_decisions d ON d.uuid=l.decision_uuid
 WHERE i.canonical_uuid=? AND d.state IN ('unlinked','undecided'))`, post); err != nil {
		return nil, false, err
	}
	if captured || rejected {
		return nil, true, nil
	}
	members, err := sourceThreadMembers(ctx, post)
	if err != nil || len(members) > 1 {
		return nil, len(members) > 1, err
	}
	states, err := sourcePostMediaStates(ctx, post)
	if err != nil {
		return nil, false, err
	}
	requested := make(map[string]bool)
	for id, state := range states {
		if state == "conflict" {
			return nil, true, nil
		}
		if state == "linked" {
			requested[id] = true
		}
	}
	resolved, err := sourceGalleryIdentities(ctx, requested)
	if err != nil {
		return nil, false, err
	}
	count := 0
	for _, media := range resolved {
		if archiveMedia(media) && media.State == models.ArchiveEntityActive {
			count++
		}
	}
	if count < 2 {
		return nil, false, nil
	}
	capture, err := sourceAlbumMetadataAnchor(ctx, post)
	if err != nil || capture == "" {
		return nil, capture == "", err
	}
	// The partial list records only that this post is an album. Its members
	// remain the existing post-media decisions: no attachment IDs, positions,
	// expected total, or new remote capture are manufactured.
	return &models.AttachmentSelection{DeclaredAlbum: true, Entries: []models.SourceAttachmentManifestEntry{},
		Decision: models.AttachmentSelectionDecision{PostUUID: post, Mode: "automatic", Origin: "migration",
			Reason: linkedMediaAlbumReason, CaptureUUID: &capture, ManifestUUIDs: []string{}}}, false, nil
}
