package sqlite

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

type ArchiveReviewStore struct{}

func (s *ArchiveReviewStore) Queue(ctx context.Context, filter models.ArchiveReviewFilter) (*models.ArchiveReviewPage, error) {
	if filter.Limit < 1 || filter.Limit > 50 || (filter.After != "" && !validSourceRunUUID(filter.After)) {
		return nil, models.ErrArchiveReviewInvalid
	}
	page := &models.ArchiveReviewPage{Kind: filter.Kind, Items: []models.ArchiveReviewItem{}}
	switch filter.Kind {
	case "accounts":
		rows, err := (&SourceAccountStore{}).ReviewAccounts(ctx, models.AccountReviewFilter{After: filter.After, Limit: filter.Limit + 1, Ownership: models.AccountOwnershipUndecided})
		if err != nil {
			return nil, err
		}
		if len(rows) > filter.Limit {
			rows = rows[:filter.Limit]
			page.Next = rows[len(rows)-1].UUID
		}
		for _, row := range rows {
			page.Items = append(page.Items, models.ArchiveReviewItem{UUID: row.UUID, Reasons: []string{"account_owner_undecided"}, Account: &row})
		}
		page.Checked = len(rows)
		return page, nil
	case "media":
		return s.media(ctx, filter, page)
	case "metadata":
		return s.metadata(ctx, filter, page)
	default:
		return nil, models.ErrArchiveReviewInvalid
	}
}

// Ordinary evidence avoids joining every media identity. Merged media and
// posts have separate, small candidate branches. A raw settled decision cannot
// suppress a conflict introduced by either kind of merge.
const archiveReviewUnselectedEvidence = `SELECT DISTINCT i.canonical_uuid AS post_uuid FROM source_post_identities i
CROSS JOIN source_media_evidence e INDEXED BY source_media_evidence_post ON e.post_uuid=i.post_uuid
WHERE i.canonical_uuid>? AND NOT EXISTS(SELECT 1 FROM post_media_links l
JOIN post_media_decisions d ON d.uuid=l.decision_uuid WHERE l.post_uuid=e.post_uuid AND l.media_uuid=e.media_uuid AND d.state IN ('linked','unlinked'))
ORDER BY i.canonical_uuid LIMIT ?`

const archiveReviewUndecidedPostChoices = `SELECT DISTINCT i.canonical_uuid AS post_uuid FROM post_media_links l
JOIN post_media_decisions d ON d.uuid=l.decision_uuid
JOIN source_post_identities i ON i.post_uuid=l.post_uuid
WHERE i.canonical_uuid>? AND d.state='undecided' ORDER BY i.canonical_uuid LIMIT ?`

const archiveReviewMergedMediaChoices = `SELECT DISTINCT i.canonical_uuid AS post_uuid FROM archive_entities m INDEXED BY archive_entities_redirect
CROSS JOIN post_media_links l ON l.media_uuid=m.uuid
JOIN source_post_identities i ON i.post_uuid=l.post_uuid
WHERE m.redirect_to IS NOT NULL AND m.kind IN ('scene','image') AND i.canonical_uuid>?
ORDER BY i.canonical_uuid LIMIT ?`

const archiveReviewMergedPosts = `SELECT DISTINCT i.canonical_uuid AS post_uuid FROM source_post_consolidations c
CROSS JOIN source_post_identities i ON i.post_uuid=c.source_uuid
WHERE i.canonical_uuid>? ORDER BY i.canonical_uuid LIMIT ?`

const archiveReviewUndecidedAttachments = `SELECT DISTINCT i.canonical_uuid AS post_uuid FROM attachment_media_links l
JOIN attachment_media_decisions d ON d.uuid=l.decision_uuid
JOIN source_attachments a ON a.uuid=l.attachment_uuid
JOIN source_post_identities i ON i.post_uuid=a.post_uuid
WHERE i.canonical_uuid>? AND d.state='undecided' ORDER BY i.canonical_uuid LIMIT ?`

func archiveReviewMediaQuery(after string, limit int) (string, []any) {
	queries := []string{archiveReviewUnselectedEvidence, archiveReviewUndecidedPostChoices,
		archiveReviewMergedMediaChoices, archiveReviewMergedPosts, archiveReviewUndecidedAttachments}
	parts, args := []string{}, []any{}
	for _, query := range queries {
		parts = append(parts, "SELECT post_uuid FROM ("+query+")")
		args = append(args, after, limit)
	}
	args = append(args, limit)
	return "SELECT post_uuid FROM (" + strings.Join(parts, " UNION ") + ") ORDER BY post_uuid LIMIT ?", args
}

func (s *ArchiveReviewStore) media(ctx context.Context, filter models.ArchiveReviewFilter, page *models.ArchiveReviewPage) (*models.ArchiveReviewPage, error) {
	const candidateLimit = 100
	query, args := archiveReviewMediaQuery(filter.After, candidateLimit+1)
	var ids []string
	if err := dbWrapper.Select(ctx, &ids, query, args...); err != nil {
		return nil, err
	}
	for index, id := range ids[:min(len(ids), candidateLimit)] {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page.Checked++
		post, err := currentSourcePost(ctx, id)
		if err != nil {
			return nil, err
		}
		if post == nil || post.UUID != id {
			return nil, models.ErrSourcePayloadCorrupt
		}
		if post.State == "active" {
			reasons, err := archiveReviewMediaReasons(ctx, post.UUID)
			if errors.Is(err, models.ErrSourceAlbumLimit) || errors.Is(err, models.ErrSourcePostIdentityLimit) {
				reasons, err = []string{"review_limit"}, nil
			}
			if err != nil {
				return nil, err
			}
			if len(reasons) != 0 {
				summary, err := (&SourceEvidenceStore{}).postSummary(ctx, post)
				if err != nil {
					return nil, err
				}
				page.Items = append(page.Items, models.ArchiveReviewItem{UUID: id, Reasons: reasons, Post: summary})
			}
		}
		if index+1 < len(ids) {
			page.Next = id
		} else {
			page.Next = ""
		}
		if len(page.Items) == filter.Limit {
			break
		}
	}
	return page, nil
}

// Inspect one canonical post with bounded reads. This mirrors the source review
// semantics: a post decision has precedence, and only an unambiguous current
// attachment link supplies the default when there is no explicit post choice.
func archiveReviewMediaReasons(ctx context.Context, post string) ([]string, error) {
	bound := maxSourceGalleryMembers + 1
	var mediaIDs []string
	if err := dbWrapper.Select(ctx, &mediaIDs, sourcePostBrowserMediaQuery, post, bound, post, bound, post, bound, bound); err != nil {
		return nil, err
	}
	if len(mediaIDs) > maxSourceGalleryMembers {
		return nil, models.ErrSourceAlbumLimit
	}
	requested := map[string]bool{}
	for _, id := range mediaIDs {
		requested[id] = true
	}
	resolved, err := sourceGalleryIdentities(ctx, requested)
	if err != nil {
		return nil, err
	}
	var choices []sourcePostMediaRow
	if err := dbWrapper.Select(ctx, &choices, `SELECT d.media_uuid,d.state FROM source_post_identities i
CROSS JOIN post_media_links l ON l.post_uuid=i.post_uuid JOIN post_media_decisions d ON d.uuid=l.decision_uuid
WHERE i.canonical_uuid=? LIMIT ?`, post, bound); err != nil {
		return nil, err
	}
	if len(choices) > maxSourceGalleryMembers {
		return nil, models.ErrSourceAlbumLimit
	}
	states := resolvedPostMediaStates(choices, resolved)
	var attachments []struct {
		Count  int    `db:"count"`
		States int    `db:"states"`
		State  string `db:"state"`
		Media  string `db:"media"`
	}
	if err := dbWrapper.Select(ctx, &attachments, `SELECT count(*) AS count,count(DISTINCT d.state) AS states,max(d.state) AS state,coalesce(max(d.media_uuid),'') AS media
FROM source_post_identities i CROSS JOIN source_attachments a ON a.post_uuid=i.post_uuid
JOIN attachment_media_links l ON l.attachment_uuid=a.uuid
JOIN attachment_media_decisions d ON d.uuid=l.decision_uuid
WHERE i.canonical_uuid=? GROUP BY a.namespace,a.value LIMIT ?`, post, bound); err != nil {
		return nil, err
	}
	if len(attachments) > maxSourceGalleryMembers {
		return nil, models.ErrSourceAlbumLimit
	}
	reasons, linked := map[string]bool{}, map[string]bool{}
	for _, attachment := range attachments {
		switch {
		case attachment.Count > 1 && (attachment.States > 1 || attachment.State != "unlinked"):
			reasons["attachment_link_conflict"] = true
		case attachment.State == "undecided":
			reasons["attachment_link_undecided"] = true
		case attachment.Count == 1 && attachment.State == "linked":
			media := resolved[attachment.Media]
			if !archiveMedia(media) {
				return nil, models.ErrSourcePayloadCorrupt
			}
			if media.State == models.ArchiveEntityActive {
				linked[media.UUID] = true
			}
		}
	}
	for _, media := range resolved {
		if !archiveMedia(media) {
			return nil, models.ErrSourcePayloadCorrupt
		}
		if media.State != models.ArchiveEntityActive {
			continue
		}
		switch states[media.UUID] {
		case "conflict":
			reasons["post_link_conflict"] = true
		case "linked", "unlinked":
		default:
			if !linked[media.UUID] {
				reasons["media_unselected"] = true
			}
		}
	}
	result := []string{}
	for reason := range reasons {
		result = append(result, reason)
	}
	slices.Sort(result)
	return result, nil
}

// File-history indexes find retained edits and their existing library owners.
// Reviewed fields remain reviewed after a later library edit. Other fields in
// that same historical edit remain candidates; request bodies are never loaded.
const archiveReviewMetadataQuery = `WITH owners AS (
 SELECT e.uuid,h.uuid AS history_uuid,ed.field FROM source_file_history_edits ed
 CROSS JOIN source_file_history h ON h.uuid=ed.history_uuid
 CROSS JOIN source_file_history_locations loc ON loc.history_uuid=h.uuid
 CROSS JOIN source_file_matches m ON m.observation_uuid=loc.observation_uuid
 CROSS JOIN archive_entities f ON f.uuid=m.file_uuid AND f.kind='file' AND f.state='active'
 CROSS JOIN scenes_files sf ON sf.file_id=f.file_id
 CROSS JOIN archive_entities e ON e.scene_id=sf.scene_id AND e.state='active'
 WHERE e.uuid>?
 UNION
 SELECT e.uuid,h.uuid AS history_uuid,ed.field FROM source_file_history_edits ed
 CROSS JOIN source_file_history h ON h.uuid=ed.history_uuid
 CROSS JOIN source_file_history_locations loc ON loc.history_uuid=h.uuid
 CROSS JOIN source_file_matches m ON m.observation_uuid=loc.observation_uuid
 CROSS JOIN archive_entities f ON f.uuid=m.file_uuid AND f.kind='file' AND f.state='active'
 CROSS JOIN images_files imf ON imf.file_id=f.file_id
 CROSS JOIN archive_entities e ON e.image_id=imf.image_id AND e.state='active'
 WHERE e.uuid>?
)
SELECT DISTINCT o.uuid FROM owners o WHERE NOT EXISTS (
 SELECT 1 FROM metadata_file_edit_reviews r JOIN metadata_field_decisions d ON d.uuid=r.decision_uuid
 WHERE r.history_uuid=o.history_uuid AND r.source_field=o.field AND d.entity_uuid IN (
 WITH RECURSIVE identities(uuid) AS (SELECT o.uuid UNION SELECT a.uuid FROM archive_entities a JOIN identities i ON a.redirect_to=i.uuid)
 SELECT uuid FROM identities)
 ) AND NOT EXISTS (
 SELECT 1 FROM metadata_file_edit_keeps k WHERE k.history_uuid=o.history_uuid AND k.source_field=o.field AND k.entity_uuid IN (
 WITH RECURSIVE identities(uuid) AS (SELECT o.uuid UNION SELECT a.uuid FROM archive_entities a JOIN identities i ON a.redirect_to=i.uuid)
 SELECT uuid FROM identities)
) ORDER BY o.uuid LIMIT ?`

func (s *ArchiveReviewStore) metadata(ctx context.Context, filter models.ArchiveReviewFilter, page *models.ArchiveReviewPage) (*models.ArchiveReviewPage, error) {
	var ids []string
	if err := dbWrapper.Select(ctx, &ids, archiveReviewMetadataQuery, filter.After, filter.After, filter.Limit+1); err != nil {
		return nil, err
	}
	if len(ids) > filter.Limit {
		ids = ids[:filter.Limit]
		page.Next = ids[len(ids)-1]
	}
	for _, id := range ids {
		entity, err := (&ArchiveEntityStore{}).Find(ctx, id)
		if err != nil {
			return nil, err
		}
		if !archiveMedia(entity) || entity.State != models.ArchiveEntityActive {
			return nil, models.ErrSourcePayloadCorrupt
		}
		media, err := sourcePostLibraryItem(ctx, entity)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, models.ArchiveReviewItem{UUID: id, Reasons: []string{"retained_metadata"}, Media: media})
	}
	page.Checked = len(ids)
	return page, nil
}
