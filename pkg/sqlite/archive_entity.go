package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type ArchiveEntityStore struct{}

const archiveUUIDExpression = `(lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))))`

type archiveEntityRow struct {
	UUID        string                    `db:"uuid"`
	Kind        models.ArchiveEntityKind  `db:"kind"`
	State       models.ArchiveEntityState `db:"state"`
	Revision    int                       `db:"revision"`
	PerformerID sql.NullInt64             `db:"performer_id"`
	SceneID     sql.NullInt64             `db:"scene_id"`
	ImageID     sql.NullInt64             `db:"image_id"`
	FileID      sql.NullInt64             `db:"file_id"`
	GalleryID   sql.NullInt64             `db:"gallery_id"`
	OriginalID  sql.NullInt64             `db:"original_id"`
	RedirectTo  sql.NullString            `db:"redirect_to"`
	CreatedAt   Timestamp                 `db:"created_at"`
	RetiredAt   sql.NullTime              `db:"retired_at"`
}

func (r archiveEntityRow) resolve() *models.ArchiveEntity {
	ret := &models.ArchiveEntity{UUID: r.UUID, Kind: r.Kind, State: r.State, Revision: r.Revision, CreatedAt: r.CreatedAt.Timestamp}
	for _, local := range []sql.NullInt64{r.PerformerID, r.SceneID, r.ImageID, r.FileID, r.GalleryID} {
		if local.Valid {
			id := int(local.Int64)
			ret.LocalID = &id
		}
	}
	if r.OriginalID.Valid {
		id := int(r.OriginalID.Int64)
		ret.OriginalID = &id
	}
	if r.RedirectTo.Valid {
		ret.RedirectTo = &r.RedirectTo.String
	}
	if r.RetiredAt.Valid {
		ret.RetiredAt = &r.RetiredAt.Time
	}
	return ret
}

func archiveUUID(value string) (string, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return "", errors.New("invalid archive UUID")
	}
	return id.String(), nil
}

func archiveLocalColumn(kind models.ArchiveEntityKind) (string, error) {
	switch kind {
	case models.ArchivePerformer:
		return "performer_id", nil
	case models.ArchiveScene:
		return "scene_id", nil
	case models.ArchiveImage:
		return "image_id", nil
	case models.ArchiveFile:
		return "file_id", nil
	case models.ArchiveGallery:
		return "gallery_id", nil
	default:
		return "", errors.New("invalid archive entity kind")
	}
}

func (s *ArchiveEntityStore) Find(ctx context.Context, value string) (*models.ArchiveEntity, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	var row archiveEntityRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM archive_entities WHERE uuid = ?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func (s *ArchiveEntityStore) FindByLocalID(ctx context.Context, kind models.ArchiveEntityKind, id int) (*models.ArchiveEntity, error) {
	column, err := archiveLocalColumn(kind)
	if err != nil {
		return nil, err
	}
	var row archiveEntityRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM archive_entities WHERE "+column+" = ?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func (s *ArchiveEntityStore) Resolve(ctx context.Context, value string) (*models.ArchiveEntity, error) {
	seen := make(map[string]bool)
	for range 128 {
		entity, err := s.Find(ctx, value)
		if err != nil || entity == nil {
			return entity, err
		}
		if seen[entity.UUID] {
			return nil, errors.New("archive identity redirect cycle")
		}
		seen[entity.UUID] = true
		if entity.RedirectTo == nil {
			return entity, nil
		}
		value = *entity.RedirectTo
	}
	return nil, errors.New("archive identity redirect chain exceeds limit")
}

func (s *ArchiveEntityStore) AdoptUUID(ctx context.Context, currentUUID, importedUUID string, expectedRevision int) (*models.ArchiveEntity, error) {
	current, err := s.Find(ctx, currentUUID)
	if err != nil {
		return nil, err
	}
	if current == nil || current.State != models.ArchiveEntityActive || current.Revision != expectedRevision {
		return nil, models.ErrArchiveIdentityConflict
	}
	importedUUID, err = archiveUUID(importedUUID)
	if err != nil {
		return nil, err
	}
	if current.UUID == importedUUID {
		return current, nil
	}
	if existing, err := s.Find(ctx, importedUUID); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, models.ErrArchiveIdentityConflict
	}
	// Foreign keys follow the new canonical UUID; retaining the previous UUID
	// then keeps old links and receipts resolvable. Both statements share the
	// caller's transaction, including the import receipt.
	result, err := dbWrapper.Exec(ctx, `UPDATE archive_entities SET uuid = ?, revision = revision + 1
WHERE uuid = ? AND revision = ? AND state = 'active'`, importedUUID, current.UUID, expectedRevision)
	if err := checkArchiveIdentityUpdate(result, err); err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO archive_entities(uuid, kind, state, original_id, redirect_to, created_at, retired_at)
VALUES (?, ?, 'redirected', ?, ?, ?, CURRENT_TIMESTAMP)`, current.UUID, current.Kind, current.OriginalID, importedUUID, Timestamp{Timestamp: current.CreatedAt})
	if err != nil {
		return nil, err
	}
	return s.Find(ctx, importedUUID)
}

func (s *ArchiveEntityStore) Redirect(ctx context.Context, sourceUUID, destinationUUID string, expectedRevision int) error {
	source, err := s.Find(ctx, sourceUUID)
	if err != nil {
		return err
	}
	destination, err := s.Resolve(ctx, destinationUUID)
	if err != nil {
		return err
	}
	if source == nil || destination == nil || source.State != models.ArchiveEntityActive || destination.State != models.ArchiveEntityActive || source.Revision != expectedRevision {
		return models.ErrArchiveIdentityConflict
	}
	if source.UUID == destination.UUID || source.Kind != destination.Kind {
		return errors.New("merge requires distinct archive entities of the same kind")
	}
	result, err := dbWrapper.Exec(ctx, `UPDATE archive_entities SET state = 'redirected', revision = revision + 1,
performer_id = NULL, scene_id = NULL, image_id = NULL, file_id = NULL, gallery_id = NULL, redirect_to = ?, retired_at = CURRENT_TIMESTAMP
WHERE uuid = ? AND revision = ? AND state = 'active'`, destination.UUID, source.UUID, expectedRevision)
	if err := checkArchiveIdentityUpdate(result, err); err != nil {
		return err
	}
	column, err := archiveLocalColumn(source.Kind)
	if err != nil {
		return err
	}
	table := map[models.ArchiveEntityKind]string{
		models.ArchivePerformer: "performers", models.ArchiveScene: "scenes",
		models.ArchiveImage: "images", models.ArchiveFile: "files",
		models.ArchiveGallery: "galleries",
	}[source.Kind]
	// The merge caller must remove the source record before commit. Refuse a
	// partial merge that would leave a library row without a current UUID.
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		var orphan bool
		err := dbWrapper.Get(ctx, &orphan, "SELECT EXISTS(SELECT 1 FROM "+table+" e WHERE e.id = ? AND NOT EXISTS (SELECT 1 FROM archive_entities a WHERE a."+column+" = e.id))", *source.LocalID)
		if err != nil {
			return err
		}
		if orphan {
			return errors.New("archive merge left its source record without a current identity")
		}
		return nil
	})
	return nil
}

func checkArchiveIdentityUpdate(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("%w: update affected %d records", models.ErrArchiveIdentityConflict, n)
	}
	return nil
}

func redirectArchiveLocalIDs(ctx context.Context, kind models.ArchiveEntityKind, sources []int, destination int) error {
	store := &ArchiveEntityStore{}
	dest, err := store.FindByLocalID(ctx, kind, destination)
	if err != nil {
		return err
	}
	if dest == nil {
		return errors.New("merge destination has no archive identity")
	}
	for _, id := range sources {
		source, err := store.FindByLocalID(ctx, kind, id)
		if err != nil {
			return err
		}
		if source == nil {
			return errors.New("merge source has no archive identity")
		}
		if err := store.Redirect(ctx, source.UUID, dest.UUID, source.Revision); err != nil {
			return err
		}
	}
	return nil
}
