package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type SourceGalleryStore struct{ gallery *GalleryStore }

func sourceGalleryPathless(ctx context.Context, id int) (bool, error) {
	var ret bool
	err := dbWrapper.Get(ctx, &ret, `SELECT EXISTS(SELECT 1 FROM galleries g
WHERE g.id = ? AND g.origin != 'filesystem' AND g.folder_id IS NULL
AND NOT EXISTS (SELECT 1 FROM galleries_files WHERE gallery_id = g.id))`, id)
	return ret, err
}

type sourceGalleryDecisionRow struct {
	UUID          string         `db:"uuid"`
	PostUUID      string         `db:"post_uuid"`
	Revision      int            `db:"revision"`
	State         string         `db:"state"`
	GalleryUUID   sql.NullString `db:"gallery_uuid"`
	SelectionUUID sql.NullString `db:"selection_uuid"`
	Origin        string         `db:"origin"`
	Reason        string         `db:"reason"`
	CreatedAt     Timestamp      `db:"created_at"`
}

func (r sourceGalleryDecisionRow) resolve() *models.SourceGalleryDecision {
	ret := &models.SourceGalleryDecision{UUID: r.UUID, PostUUID: r.PostUUID, Revision: r.Revision, State: r.State, Origin: r.Origin, Reason: r.Reason, CreatedAt: r.CreatedAt.Timestamp}
	if r.GalleryUUID.Valid {
		ret.GalleryUUID = &r.GalleryUUID.String
	}
	if r.SelectionUUID.Valid {
		ret.SelectionUUID = &r.SelectionUUID.String
	}
	return ret
}

func (s *SourceGalleryStore) Association(ctx context.Context, value string) (*models.SourceGalleryDecision, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	var row sourceGalleryDecisionRow
	if err := dbWrapper.Get(ctx, &row, `SELECT d.* FROM post_gallery_links l
JOIN post_gallery_decisions d ON d.post_uuid = l.post_uuid AND d.uuid = l.decision_uuid WHERE l.post_uuid = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func (s *SourceGalleryStore) AssociationHistory(ctx context.Context, value string, after, limit int) ([]models.SourceGalleryDecision, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, errors.New("invalid source gallery decision cursor")
	}
	var rows []sourceGalleryDecisionRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM post_gallery_decisions WHERE post_uuid = ? AND revision > ? ORDER BY revision LIMIT ?", id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.SourceGalleryDecision, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, *row.resolve())
	}
	return ret, nil
}

func (s *SourceGalleryStore) DecideAssociation(ctx context.Context, input models.SourceGalleryChoiceInput) (*models.SourceGalleryDecision, error) {
	if input.Origin != "review" && input.Origin != "migration" {
		return nil, errors.New("source gallery adoption requires review or migration")
	}
	return s.decideAssociation(ctx, input, nil)
}

func (s *SourceGalleryStore) decideAssociation(ctx context.Context, input models.SourceGalleryChoiceInput, selection *string) (*models.SourceGalleryDecision, error) {
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, input.PostUUID)
	if err != nil {
		return nil, err
	}
	if post == nil || post.Revision != input.ExpectedPostRevision {
		return nil, models.ErrSourceGalleryConflict
	}
	if post.State != "active" {
		return nil, models.ErrSourcePostForgotten
	}
	if !validAccountText(input.Reason, 4096, true) {
		return nil, errors.New("invalid source gallery decision reason")
	}
	var galleryUUID *string
	switch input.State {
	case "linked":
		identity, err := (&ArchiveEntityStore{}).Find(ctx, input.GalleryUUID)
		if err != nil {
			return nil, err
		}
		if identity == nil || identity.Kind != models.ArchiveGallery || identity.State != models.ArchiveEntityActive || identity.Revision != input.ExpectedGalleryRevision {
			return nil, models.ErrSourceGalleryConflict
		}
		pathless, err := sourceGalleryPathless(ctx, *identity.LocalID)
		if err != nil {
			return nil, err
		}
		if !pathless {
			return nil, errors.New("source albums require a non-filesystem gallery without a folder or ZIP")
		}
		var claimed bool
		if err := dbWrapper.Get(ctx, &claimed, "SELECT EXISTS(SELECT 1 FROM post_gallery_links WHERE gallery_uuid = ? AND post_uuid != ?)", identity.UUID, post.UUID); err != nil {
			return nil, err
		}
		if claimed {
			return nil, models.ErrSourceGalleryConflict
		}
		galleryUUID = &identity.UUID
	case "disabled":
		if input.GalleryUUID != "" || input.ExpectedGalleryRevision != 0 {
			return nil, errors.New("disabled source gallery cannot select a gallery")
		}
	default:
		return nil, errors.New("invalid source gallery decision")
	}
	result, err := dbWrapper.Exec(ctx, "UPDATE source_posts SET revision = revision + 1 WHERE uuid = ? AND revision = ?", post.UUID, post.Revision)
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, models.ErrSourceGalleryConflict
	}
	id := uuid.NewString()
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO post_gallery_decisions(uuid, post_uuid, revision, state, gallery_uuid, selection_uuid, origin, reason)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, post.UUID, post.Revision+1, input.State, galleryUUID, selection, input.Origin, input.Reason); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO post_gallery_links(post_uuid, decision_uuid, gallery_uuid) VALUES (?, ?, ?)
ON CONFLICT(post_uuid) DO UPDATE SET decision_uuid = excluded.decision_uuid, gallery_uuid = excluded.gallery_uuid`, post.UUID, id, galleryUUID); err != nil {
		return nil, err
	}
	return s.Association(ctx, post.UUID)
}

type galleryMembershipEventRow struct {
	UUID          string         `db:"uuid"`
	Sequence      int            `db:"sequence"`
	GalleryUUID   string         `db:"gallery_uuid"`
	MediaUUID     string         `db:"media_uuid"`
	State         string         `db:"state"`
	Origin        string         `db:"origin"`
	PostUUID      sql.NullString `db:"post_uuid"`
	SelectionUUID sql.NullString `db:"selection_uuid"`
	CreatedAt     Timestamp      `db:"created_at"`
}

func (r galleryMembershipEventRow) resolve() models.GalleryMembershipEvent {
	ret := models.GalleryMembershipEvent{UUID: r.UUID, Sequence: r.Sequence, GalleryUUID: r.GalleryUUID, MediaUUID: r.MediaUUID, State: r.State, Origin: r.Origin, CreatedAt: r.CreatedAt.Timestamp}
	if r.PostUUID.Valid {
		ret.PostUUID = &r.PostUUID.String
	}
	if r.SelectionUUID.Valid {
		ret.SelectionUUID = &r.SelectionUUID.String
	}
	return ret
}

func (s *SourceGalleryStore) MembershipHistory(ctx context.Context, value string, after, limit int) ([]models.GalleryMembershipEvent, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, errors.New("invalid membership history cursor")
	}
	var rows []galleryMembershipEventRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM gallery_membership_events WHERE gallery_uuid = ? AND sequence > ? ORDER BY sequence LIMIT ?", id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.GalleryMembershipEvent, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.resolve())
	}
	return ret, nil
}

// SQLite serializes writers. This marker belongs to the caller's transaction,
// is scoped to one gallery/post, and must be removed before commit. Relationship
// triggers can therefore distinguish source work from ordinary library edits,
// including writes arriving through image/scene-side APIs.
func withSourceGalleryWrite(ctx context.Context, gallery, post, selection string, fn func() error) error {
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_gallery_write_context(gallery_uuid, post_uuid, selection_uuid) VALUES (?, ?, ?)", gallery, post, selection); err != nil {
		return err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		var leftover bool
		if err := dbWrapper.Get(ctx, &leftover, "SELECT EXISTS(SELECT 1 FROM source_gallery_write_context)"); err != nil {
			return err
		}
		if leftover {
			return errors.New("source gallery write context was not closed")
		}
		return nil
	})
	err := fn()
	_, cleanup := dbWrapper.Exec(ctx, "DELETE FROM source_gallery_write_context WHERE gallery_uuid = ?", gallery)
	return errors.Join(err, cleanup)
}
