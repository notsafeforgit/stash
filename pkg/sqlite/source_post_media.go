package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/stashapp/stash/pkg/models"
)

type SourcePostMediaStore struct{ gallery *GalleryStore }

type sourcePostMediaRow struct {
	UUID          string    `db:"uuid"`
	PostUUID      string    `db:"post_uuid"`
	MediaUUID     string    `db:"media_uuid"`
	PostRevision  int       `db:"post_revision"`
	MediaRevision int       `db:"media_revision"`
	State         string    `db:"state"`
	Origin        string    `db:"origin"`
	Reason        string    `db:"reason"`
	RequestDigest string    `db:"request_digest"`
	CreatedAt     Timestamp `db:"created_at"`
}

func (r sourcePostMediaRow) resolve() *models.SourcePostMediaDecision {
	return &models.SourcePostMediaDecision{UUID: r.UUID, PostUUID: r.PostUUID, MediaUUID: r.MediaUUID,
		PostRevision: r.PostRevision, MediaRevision: r.MediaRevision, State: r.State, Origin: r.Origin, Reason: r.Reason, CreatedAt: r.CreatedAt.Timestamp}
}

// Reverse redirects are indexed and bounded. Truncating them would hide
// conflicting choices after a merge, so an oversized group requires review.
func sourceMediaAliases(ctx context.Context, media string) ([]string, error) {
	var ids []string
	err := dbWrapper.Select(ctx, &ids, `WITH RECURSIVE identities(uuid) AS (
SELECT ? UNION SELECT e.uuid FROM archive_entities e JOIN identities i ON e.redirect_to=i.uuid LIMIT 1025
) SELECT uuid FROM identities ORDER BY uuid`, media)
	if err != nil {
		return nil, err
	}
	if len(ids) > 1024 {
		return nil, fmt.Errorf("%w: more than 1024 merged identities", models.ErrSourcePostMediaInvalid)
	}
	return ids, nil
}

func sourcePostMediaRows(ctx context.Context, post string, ids []string) ([]sourcePostMediaRow, error) {
	args := []interface{}{post}
	for _, id := range ids {
		args = append(args, id)
	}
	var rows []sourcePostMediaRow
	err := dbWrapper.Select(ctx, &rows, `SELECT d.* FROM post_media_links l JOIN post_media_decisions d ON d.uuid=l.decision_uuid
WHERE l.post_uuid=? AND l.media_uuid IN `+getInBinding(len(ids))+` ORDER BY d.post_revision`, args...)
	return rows, err
}

func postMediaState(rows []sourcePostMediaRow) string {
	state := "undecided"
	for i, r := range rows {
		if i > 0 && r.State != state {
			return "conflict"
		}
		state = r.State
	}
	return state
}

func resolvedPostMediaStates(rows []sourcePostMediaRow, resolved map[string]*models.ArchiveEntity) map[string]string {
	ret := make(map[string]string)
	for _, choice := range rows {
		id := resolved[choice.MediaUUID].UUID
		if old, exists := ret[id]; exists && old != choice.State {
			ret[id] = "conflict"
		} else {
			ret[id] = choice.State
		}
	}
	return ret
}

// Album matching inspects the selected post once, rather than issuing a
// separate redirect/history lookup for every attachment candidate.
func sourcePostMediaStates(ctx context.Context, post string) (map[string]string, error) {
	var rows []sourcePostMediaRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT d.* FROM post_media_links l
JOIN post_media_decisions d ON d.uuid=l.decision_uuid WHERE l.post_uuid=? ORDER BY l.media_uuid LIMIT ?`, post, maxSourceGalleryMembers+1); err != nil {
		return nil, err
	}
	if len(rows) > maxSourceGalleryMembers {
		return nil, models.ErrSourceAlbumLimit
	}
	requested := make(map[string]bool)
	for _, row := range rows {
		requested[row.MediaUUID] = true
	}
	resolved, err := sourceGalleryIdentities(ctx, requested)
	if err != nil {
		return nil, err
	}
	return resolvedPostMediaStates(rows, resolved), nil
}

func (s *SourcePostMediaStore) Association(ctx context.Context, postID, mediaID string) (*models.SourcePostMediaAssociation, error) {
	if sourceFileIDs(&postID, &mediaID) != nil {
		return nil, models.ErrSourcePostMediaInvalid
	}
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, postID)
	if err != nil {
		return nil, err
	}
	media, err := (&ArchiveEntityStore{}).Resolve(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	if post == nil || !archiveMedia(media) {
		return nil, models.ErrSourcePostMediaInvalid
	}
	ids, err := sourceMediaAliases(ctx, media.UUID)
	if err != nil {
		return nil, err
	}
	rows, err := sourcePostMediaRows(ctx, post.UUID, ids)
	if err != nil {
		return nil, err
	}
	ret := &models.SourcePostMediaAssociation{PostUUID: post.UUID, PostRevision: post.Revision, PostState: post.State, MediaUUID: media.UUID, MediaRevision: media.Revision, MediaState: media.State, State: postMediaState(rows), Decisions: []models.SourcePostMediaDecision{}}
	for _, r := range rows {
		ret.Decisions = append(ret.Decisions, *r.resolve())
	}
	return ret, nil
}

func findPostMediaDecision(ctx context.Context, id string) (*sourcePostMediaRow, error) {
	if sourceFileIDs(&id) != nil {
		return nil, models.ErrSourcePostMediaInvalid
	}
	var r sourcePostMediaRow
	if err := dbWrapper.Get(ctx, &r, "SELECT * FROM post_media_decisions WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func (s *SourcePostMediaStore) Decision(ctx context.Context, id string) (*models.SourcePostMediaDecision, error) {
	r, err := findPostMediaDecision(ctx, id)
	if err != nil || r == nil {
		return nil, err
	}
	return r.resolve(), nil
}

func (s *SourcePostMediaStore) History(ctx context.Context, post, media string, after, limit int) ([]models.SourcePostMediaDecision, error) {
	if sourceFileIDs(&post, &media) != nil || after < 0 {
		return nil, models.ErrSourcePostMediaInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	entity, err := (&ArchiveEntityStore{}).Resolve(ctx, media)
	if err != nil {
		return nil, err
	}
	if !archiveMedia(entity) {
		return nil, models.ErrSourcePostMediaInvalid
	}
	ids, err := sourceMediaAliases(ctx, entity.UUID)
	if err != nil {
		return nil, err
	}
	args := []interface{}{post, after}
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, limit)
	var rows []sourcePostMediaRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT * FROM post_media_decisions WHERE post_uuid=? AND post_revision>?
AND media_uuid IN `+getInBinding(len(ids))+` ORDER BY post_revision LIMIT ?`, args...); err != nil {
		return nil, err
	}
	ret := make([]models.SourcePostMediaDecision, 0, len(rows))
	for _, r := range rows {
		ret = append(ret, *r.resolve())
	}
	return ret, nil
}

func (s *SourcePostMediaStore) Decide(ctx context.Context, input models.SourcePostMediaInput) (*models.SourcePostMediaDecision, error) {
	return s.decide(ctx, input, true)
}

func (s *SourcePostMediaStore) decide(ctx context.Context, input models.SourcePostMediaInput, synchronize bool) (*models.SourcePostMediaDecision, error) {
	if _, err := getTx(ctx); err != nil {
		return nil, err
	}
	if sourceFileIDs(&input.UUID, &input.PostUUID, &input.MediaUUID) != nil || input.ExpectedPostRevision < 1 || input.ExpectedMediaRevision < 1 ||
		(input.Origin != "review" && input.Origin != "migration") || !validAccountText(input.Reason, 4096, true) ||
		(input.State != "linked" && input.State != "unlinked" && input.State != "undecided") {
		return nil, models.ErrSourcePostMediaInvalid
	}
	input.ExpectedDecisions = append([]string{}, input.ExpectedDecisions...)
	if len(input.ExpectedDecisions) > 1024 {
		return nil, models.ErrSourcePostMediaInvalid
	}
	for i := range input.ExpectedDecisions {
		if sourceFileIDs(&input.ExpectedDecisions[i]) != nil {
			return nil, models.ErrSourcePostMediaInvalid
		}
	}
	sort.Strings(input.ExpectedDecisions)
	if len(slices.Compact(slices.Clone(input.ExpectedDecisions))) != len(input.ExpectedDecisions) {
		return nil, models.ErrSourcePostMediaInvalid
	}
	digest, err := sourceSignature("stash-post-media-decision-v1", input)
	if err != nil {
		return nil, err
	}
	existing, err := findPostMediaDecision(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.RequestDigest != digest {
			return nil, models.ErrSourcePostMediaReplay
		}
		return existing.resolve(), nil
	}
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, input.PostUUID)
	if err != nil {
		return nil, err
	}
	media, err := (&ArchiveEntityStore{}).Find(ctx, input.MediaUUID)
	if err != nil {
		return nil, err
	}
	if post == nil || post.State != "active" || post.Revision != input.ExpectedPostRevision || !archiveMedia(media) ||
		media.State != models.ArchiveEntityActive || media.Revision != input.ExpectedMediaRevision {
		return nil, models.ErrSourcePostMediaConflict
	}
	ids, err := sourceMediaAliases(ctx, media.UUID)
	if err != nil {
		return nil, err
	}
	current, err := sourcePostMediaRows(ctx, post.UUID, ids)
	if err != nil {
		return nil, err
	}
	currentIDs := make([]string, 0, len(current))
	for _, d := range current {
		currentIDs = append(currentIDs, d.UUID)
	}
	sort.Strings(currentIDs)
	if !slices.Equal(currentIDs, input.ExpectedDecisions) {
		return nil, models.ErrSourcePostMediaConflict
	}
	result, err := dbWrapper.Exec(ctx, "UPDATE source_posts SET revision=revision+1 WHERE uuid=? AND revision=?", post.UUID, post.Revision)
	if err := checkArchiveIdentityUpdate(result, err); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO post_media_decisions(uuid,post_uuid,media_uuid,post_revision,media_revision,state,origin,reason,request_digest)
VALUES(?,?,?,?,?,?,?,?,?)`, input.UUID, post.UUID, media.UUID, post.Revision+1, media.Revision, input.State, input.Origin, input.Reason, digest); err != nil {
		return nil, err
	}
	// A fresh review resolves every current choice brought together by a merge.
	// Original choices remain immutable history under their original identity.
	for _, decision := range current {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO post_media_supersessions(previous_uuid,decision_uuid) VALUES(?,?)", decision.UUID, input.UUID); err != nil {
			return nil, err
		}
	}
	args := []interface{}{post.UUID}
	for _, id := range ids {
		args = append(args, id)
	}
	if _, err := dbWrapper.Exec(ctx, "DELETE FROM post_media_links WHERE post_uuid=? AND media_uuid IN "+getInBinding(len(ids)), args...); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO post_media_links(post_uuid,media_uuid,decision_uuid) VALUES(?,?,?)", post.UUID, media.UUID, input.UUID); err != nil {
		return nil, err
	}
	if synchronize {
		if err := s.syncGallery(ctx, post.UUID); err != nil {
			return nil, err
		}
	}
	return s.Decision(ctx, input.UUID)
}

func (s *SourcePostMediaStore) syncGallery(ctx context.Context, post string) error {
	// Never create an album from a direct link. Existing source membership may
	// need removal/restoration; Sync protects manual membership, order and cover.
	gallery := &SourceGalleryStore{gallery: s.gallery}
	association, err := gallery.Association(ctx, post)
	if err != nil {
		return err
	}
	if association != nil && association.State == "linked" {
		preview, err := gallery.Preview(ctx, post)
		if err != nil {
			return err
		}
		if preview.Action == "sync" {
			if _, err := gallery.Sync(ctx, post, preview.Signature); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SourcePostMediaStore) ValidateCapture(ctx context.Context, decisionID, captureID, mediaID string) error {
	d, err := s.Decision(ctx, decisionID)
	if err != nil {
		return err
	}
	c, err := (&SourceEvidenceStore{}).FindCapture(ctx, captureID)
	if err != nil {
		return err
	}
	if d == nil || c == nil || d.PostUUID != c.PostUUID || d.State != "linked" {
		return models.ErrSourcePostMediaConflict
	}
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, c.PostUUID)
	if err != nil {
		return err
	}
	media, err := (&ArchiveEntityStore{}).Resolve(ctx, mediaID)
	if err != nil {
		return err
	}
	if post == nil || post.State != "active" || !archiveMedia(media) || media.State != models.ArchiveEntityActive {
		return models.ErrSourcePostMediaConflict
	}
	a, err := s.Association(ctx, post.UUID, media.UUID)
	if err != nil {
		return err
	}
	if a.State == "linked" {
		for _, current := range a.Decisions {
			if current.UUID == d.UUID {
				return nil
			}
		}
	}
	return models.ErrSourcePostMediaConflict
}
