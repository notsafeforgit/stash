package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type SourceThreadStore struct{}

type sourceThreadRow struct {
	models.SourceThreadFacts
	PostUUID            string         `db:"post_uuid"`
	CaptureUUID         string         `db:"capture_uuid"`
	ConflictCaptureUUID sql.NullString `db:"conflict_capture_uuid"`
}

// Imported captures are not scanned on migration. This is called only when a
// native capture is received; repeat sightings reuse one set of relationships.
func (s *SourceThreadStore) ObserveCapture(ctx context.Context, capture *models.SourceCapture) (string, error) {
	if capture.Origin != "gallery-dl" || capture.Platform != "twitter" {
		return "unavailable", nil
	}
	var err error
	if capture.Payload == nil {
		capture, err = (&SourceEvidenceStore{}).FindCapture(ctx, capture.UUID)
		if err != nil {
			return "", err
		}
		if capture == nil {
			return "", models.ErrSourcePayloadCorrupt
		}
	}
	raw, err := archive.RestoreCapture(capture.Payload)
	if err != nil {
		return "", err
	}
	facts, err := archive.ExtractCapturedThread(raw)
	if err != nil {
		return "review", nil
	}
	if facts == nil {
		return "unavailable", nil
	}
	previous, err := sourceThreadForPost(ctx, capture.PostUUID)
	if err != nil {
		return "", err
	}
	if previous == nil {
		_, err = dbWrapper.Exec(ctx, `INSERT INTO source_post_threads(post_uuid,capture_uuid,namespace,post_id,conversation_id,author_id,reply_id,reply_author_id)
VALUES(?,?,?,?,?,?,?,?)`, capture.PostUUID, capture.UUID, facts.Namespace, facts.PostID, facts.ConversationID, facts.AuthorID, facts.ReplyID, facts.ReplyAuthorID)
		return "recorded", err
	}
	if previous.ConflictCaptureUUID.Valid {
		return "review", nil
	}
	old, next := previous.SourceThreadFacts, *facts
	old.ReplyAuthorID, next.ReplyAuthorID = "", ""
	if previous.PostUUID != capture.PostUUID || old != next ||
		(previous.ReplyAuthorID != "" && facts.ReplyAuthorID != "" && previous.ReplyAuthorID != facts.ReplyAuthorID) {
		_, err = dbWrapper.Exec(ctx, "UPDATE source_post_threads SET conflict_capture_uuid=? WHERE post_uuid=?", capture.UUID, previous.PostUUID)
		return "review", err
	}
	if previous.ReplyAuthorID == "" && facts.ReplyAuthorID != "" {
		_, err = dbWrapper.Exec(ctx, "UPDATE source_post_threads SET reply_author_id=?,capture_uuid=? WHERE post_uuid=?", facts.ReplyAuthorID, capture.UUID, previous.PostUUID)
	}
	return "recorded", err
}

func sourceThreadForPost(ctx context.Context, post string) (*sourceThreadRow, error) {
	var rows []sourceThreadRow
	err := dbWrapper.Select(ctx, &rows, `SELECT t.* FROM source_post_identities i JOIN source_post_threads t ON t.post_uuid=i.post_uuid
WHERE i.canonical_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=?) LIMIT 2`, post)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	if len(rows) > 1 {
		rows[0].ConflictCaptureUUID = sql.NullString{String: rows[1].CaptureUUID, Valid: true}
	}
	return &rows[0], nil
}

func sourceThreadLink(ctx context.Context, namespace, id string) (*models.SourceThreadLink, error) {
	if id == "" {
		return nil, nil
	}
	ret := &models.SourceThreadLink{SourceID: id, URL: "https://x.com/i/status/" + id}
	post, err := (&SourceEvidenceStore{}).FindPostByIdentifier(ctx, models.SourcePostIdentifier{Namespace: namespace, Value: id})
	if err != nil || post == nil {
		return ret, err
	}
	ret.Post, err = (&SourceEvidenceStore{}).PostSummary(ctx, post.UUID)
	return ret, err
}

func (s *SourceThreadStore) Read(ctx context.Context, id, after string, limit int) (*models.SourceThreadView, error) {
	if !validSourceRunUUID(id) || (after != "" && !archive.TwitterThreadID(after)) || limit < 1 || limit > 100 {
		return nil, models.ErrSourcePostBrowseInvalid
	}
	post, err := currentSourcePost(ctx, id)
	if err != nil || post == nil {
		return nil, err
	}
	ret := &models.SourceThreadView{PostUUID: post.UUID, Posts: []models.SourceThreadLink{}}
	row, err := sourceThreadForPost(ctx, id)
	if err != nil || row == nil {
		return ret, err
	}
	ret.Facts, ret.Conflict = &row.SourceThreadFacts, row.ConflictCaptureUUID.Valid
	ret.Root, err = sourceThreadLink(ctx, row.Namespace, row.ConversationID)
	if err != nil {
		return nil, err
	}
	ret.Parent, err = sourceThreadLink(ctx, row.Namespace, row.ReplyID)
	if err != nil {
		return nil, err
	}
	var ids []string
	err = dbWrapper.Select(ctx, &ids, `SELECT post_id FROM source_post_threads
WHERE namespace=? AND conversation_id=? AND (length(post_id),post_id)>(?,?)
ORDER BY length(post_id),post_id LIMIT ?`, row.Namespace, row.ConversationID, len(after), after, limit+1)
	if err != nil {
		return nil, err
	}
	if len(ids) > limit {
		ids = ids[:limit]
		ret.Next = ids[len(ids)-1]
	}
	for _, value := range ids {
		link, err := sourceThreadLink(ctx, row.Namespace, value)
		if err != nil {
			return nil, err
		}
		ret.Posts = append(ret.Posts, *link)
	}
	return ret, nil
}

// Same numeric publisher, same conversation and an explicit self-reply. A
// missing root is navigable via its service URL, not a fabricated local post.
func sourceThreadMembers(ctx context.Context, post string) ([]sourceThreadRow, error) {
	row, err := sourceThreadForPost(ctx, post)
	if err != nil || row == nil {
		return nil, err
	}
	if row.ConflictCaptureUUID.Valid {
		return nil, models.ErrSourceGalleryConflict
	}
	if row.ReplyID != "" && row.ReplyAuthorID != row.AuthorID {
		return nil, nil
	}
	var wrongRoot bool
	if err := dbWrapper.Get(ctx, &wrongRoot, `SELECT EXISTS(SELECT 1 FROM source_post_threads WHERE namespace=? AND post_id=? AND (author_id!=? OR conflict_capture_uuid IS NOT NULL))`, row.Namespace, row.ConversationID, row.AuthorID); err != nil {
		return nil, err
	}
	if wrongRoot {
		return nil, models.ErrSourceGalleryConflict
	}
	var rows []sourceThreadRow
	err = dbWrapper.Select(ctx, &rows, `SELECT t.* FROM source_post_threads t
JOIN source_posts p ON p.uuid=t.post_uuid JOIN source_post_identities i ON i.post_uuid=t.post_uuid
WHERE t.namespace=? AND t.conversation_id=? AND t.author_id=? AND t.conflict_capture_uuid IS NULL
AND (t.reply_id='' OR t.reply_author_id=t.author_id) AND p.state='active' AND i.canonical_uuid=t.post_uuid
ORDER BY length(t.post_id),t.post_id LIMIT 257`, row.Namespace, row.ConversationID, row.AuthorID)
	if len(rows) > 256 {
		return nil, models.ErrSourceAlbumLimit
	}
	return rows, err
}

func sourceThreadSharesGallery(ctx context.Context, gallery, post string) (bool, error) {
	members, err := sourceThreadMembers(ctx, post)
	if errors.Is(err, models.ErrSourceGalleryConflict) || errors.Is(err, models.ErrSourceAlbumLimit) {
		return false, nil
	}
	if err != nil || len(members) < 2 {
		return false, err
	}
	allowed := make(map[string]bool, len(members))
	for _, m := range members {
		allowed[m.PostUUID] = true
	}
	var posts []string
	if err := dbWrapper.Select(ctx, &posts, `SELECT post_uuid FROM post_gallery_links WHERE gallery_uuid=? LIMIT 257`, gallery); err != nil {
		return false, err
	}
	if len(posts) > 256 {
		return false, nil
	}
	for _, p := range posts {
		if !allowed[p] {
			return false, nil
		}
	}
	return len(posts) > 0, nil
}

func sourceThreadFallbackTitle(row sourceThreadRow) string {
	return "Twitter thread " + strings.TrimSpace(row.ConversationID)
}
