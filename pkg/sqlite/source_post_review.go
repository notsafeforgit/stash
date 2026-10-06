package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/stashapp/stash/pkg/models"
)

// Each branch starts from the selected media's indexes, not the post library.
// Taking the first N distinct posts in each branch preserves the global first N
// after union. Redirects retain evidence and explicit choices after media merges.
func sourceMediaPostsQuery(ids []string, after string, limit int) (string, []interface{}) {
	bindings := getInBinding(len(ids))
	args := make([]interface{}, 0, 3*(len(ids)+2)+1)
	for range 3 {
		for _, id := range ids {
			args = append(args, id)
		}
		args = append(args, after, limit)
	}
	args = append(args, limit)
	return `SELECT post_uuid FROM (
SELECT post_uuid FROM (SELECT DISTINCT post_uuid FROM source_media_evidence INDEXED BY source_media_evidence_media
 WHERE media_uuid IN ` + bindings + ` AND post_uuid>? ORDER BY post_uuid LIMIT ?)
UNION
SELECT post_uuid FROM (SELECT post_uuid FROM post_media_links INDEXED BY post_media_links_media
 WHERE media_uuid IN ` + bindings + ` AND post_uuid>? GROUP BY post_uuid ORDER BY post_uuid LIMIT ?)
UNION
SELECT post_uuid FROM (SELECT DISTINCT a.post_uuid FROM attachment_media_decisions d INDEXED BY attachment_media_decisions_media
 JOIN attachment_media_links l ON l.attachment_uuid=d.attachment_uuid AND l.decision_uuid=d.uuid
 JOIN source_attachments a ON a.uuid=d.attachment_uuid
 WHERE d.media_uuid IN ` + bindings + ` AND a.post_uuid>? ORDER BY a.post_uuid LIMIT ?)
) ORDER BY post_uuid LIMIT ?`, args
}

func (s *SourcePostMediaStore) PostsForMedia(ctx context.Context, mediaID, after string, limit int) ([]models.SourcePostMediaReview, error) {
	if sourceFileIDs(&mediaID) != nil || (after != "" && sourceFileIDs(&after) != nil) || limit < 1 || limit > 100 {
		return nil, models.ErrSourcePostMediaInvalid
	}
	media, err := (&ArchiveEntityStore{}).Resolve(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	if !archiveMedia(media) {
		return nil, models.ErrSourcePostMediaInvalid
	}
	ids, err := sourceMediaAliases(ctx, media.UUID)
	if err != nil {
		return nil, err
	}
	query, args := sourceMediaPostsQuery(ids, after, limit)
	var posts []string
	if err := dbWrapper.Select(ctx, &posts, query, args...); err != nil {
		return nil, err
	}
	ret := make([]models.SourcePostMediaReview, 0, len(posts))
	for _, post := range posts {
		item, err := s.review(ctx, post, media, ids)
		if err != nil {
			return nil, err
		}
		ret = append(ret, *item)
	}
	return ret, nil
}

func (s *SourcePostMediaStore) Review(ctx context.Context, postID, mediaID string) (*models.SourcePostMediaReview, error) {
	if sourceFileIDs(&postID, &mediaID) != nil {
		return nil, models.ErrSourcePostMediaInvalid
	}
	media, err := (&ArchiveEntityStore{}).Resolve(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	if !archiveMedia(media) {
		return nil, models.ErrSourcePostMediaInvalid
	}
	ids, err := sourceMediaAliases(ctx, media.UUID)
	if err != nil {
		return nil, err
	}
	return s.review(ctx, postID, media, ids)
}

func (s *SourcePostMediaStore) review(ctx context.Context, postID string, media *models.ArchiveEntity, ids []string) (*models.SourcePostMediaReview, error) {
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, postID)
	if err != nil {
		return nil, err
	}
	if post == nil {
		return nil, models.ErrSourcePostMediaInvalid
	}
	choices, err := sourcePostMediaRows(ctx, postID, ids)
	if err != nil {
		return nil, err
	}
	a := &models.SourcePostMediaAssociation{PostUUID: postID, PostRevision: post.Revision, PostState: post.State,
		MediaUUID: media.UUID, MediaRevision: media.Revision, MediaState: media.State, State: postMediaState(choices), Decisions: []models.SourcePostMediaDecision{}}
	for _, choice := range choices {
		a.Decisions = append(a.Decisions, *choice.resolve())
	}
	ret := &models.SourcePostMediaReview{Association: a}
	ret.LatestCapture, err = sourceReviewLatestCapture(ctx, postID)
	if err != nil {
		return nil, err
	}
	ret.URLs, err = (&SourcePostLinksStore{}).URLs(ctx, postID, "", 4)
	if err != nil {
		return nil, err
	}
	if len(ret.URLs) > 3 {
		ret.URLs, ret.MoreURLs = ret.URLs[:3], true
	}
	args := []interface{}{postID}
	for _, id := range ids {
		args = append(args, id)
	}
	if err := dbWrapper.Get(ctx, &ret.HasRetainedEvidence, `SELECT EXISTS(SELECT 1 FROM source_media_evidence INDEXED BY source_media_evidence_media
WHERE post_uuid=? AND media_uuid IN `+getInBinding(len(ids))+`)`, args...); err != nil {
		return nil, err
	}
	if err := dbWrapper.Get(ctx, &ret.LinkedAttachments, `SELECT count(*) FROM source_attachments a
JOIN attachment_media_links l ON l.attachment_uuid=a.uuid
JOIN attachment_media_decisions d ON d.uuid=l.decision_uuid AND d.attachment_uuid=a.uuid
WHERE a.post_uuid=? AND d.state='linked' AND d.media_uuid IN `+getInBinding(len(ids)), args...); err != nil {
		return nil, err
	}
	return ret, nil
}

func sourceReviewLatestCapture(ctx context.Context, post string) (*models.SourcePostReviewCapture, error) {
	var row struct {
		UUID           string         `db:"uuid"`
		RevisionUUID   string         `db:"revision_uuid"`
		Origin         string         `db:"origin"`
		Platform       string         `db:"platform"`
		CapturedAt     NullTimestamp  `db:"captured_at"`
		RecordedAt     NullTimestamp  `db:"recorded_at"`
		Title          sql.NullString `db:"title"`
		PublishedAt    sql.NullString `db:"published_at"`
		DateBasis      sql.NullString `db:"date_basis"`
		TitleTruncated bool           `db:"title_truncated"`
	}
	// Project only compact metadata. This deliberately never joins source_payloads
	// or profile bodies, and uses the same observation/recording clock as Captures.
	err := dbWrapper.Get(ctx, &row, `SELECT c.uuid, c.revision_uuid, c.origin, c.platform,
c.captured_at,c.recorded_at,
substr(json_extract(r.metadata,'$.title'),1,512) AS title,
coalesce(length(json_extract(r.metadata,'$.title'))>512,0) AS title_truncated,
json_extract(r.metadata,'$.published_at') AS published_at,json_extract(r.metadata,'$.date_basis') AS date_basis
FROM source_captures c INDEXED BY source_captures_order
JOIN source_post_revisions r ON r.post_uuid=c.post_uuid AND r.uuid=c.revision_uuid
WHERE c.post_uuid=? ORDER BY coalesce(c.captured_at,c.recorded_at) DESC,c.uuid DESC LIMIT 1`, post)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stringPtr := func(value sql.NullString) *string {
		if value.Valid {
			return &value.String
		}
		return nil
	}
	return &models.SourcePostReviewCapture{UUID: row.UUID, RevisionUUID: row.RevisionUUID, Origin: row.Origin, Platform: row.Platform,
		CapturedAt: row.CapturedAt.TimePtr(), RecordedAt: row.RecordedAt.TimePtr(), Title: stringPtr(row.Title), TitleTruncated: row.TitleTruncated,
		PublishedAt: stringPtr(row.PublishedAt), DateBasis: stringPtr(row.DateBasis)}, nil
}
