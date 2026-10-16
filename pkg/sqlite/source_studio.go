package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/stashapp/stash/pkg/models"
)

type SourceStudioStore struct{}

func (s *SourceStudioStore) Assignment(ctx context.Context, collection string) (*models.SourceStudioAssignment, error) {
	c, err := (&SourceCollectionStore{}).Find(ctx, collection)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, models.ErrSourceDefinitionInvalid
	}
	if c.CanonicalUUID != "" {
		collection = c.CanonicalUUID
	}
	ret := &models.SourceStudioAssignment{CollectionUUID: collection}
	var row struct {
		Revision int            `db:"revision"`
		UUID     sql.NullString `db:"uuid"`
		Name     sql.NullString `db:"name"`
	}
	err = dbWrapper.Get(ctx, &row, `SELECT b.revision,a.uuid,s.name FROM source_collection_studios b
LEFT JOIN studios s ON s.id=b.studio_id
LEFT JOIN archive_entities a ON a.studio_id=s.id AND a.state='active'
WHERE b.collection_uuid=?`, collection)
	if errors.Is(err, sql.ErrNoRows) {
		return ret, nil
	}
	if err != nil {
		return nil, err
	}
	ret.Revision = row.Revision
	if row.UUID.Valid {
		ret.Studio = &models.SourceStudio{UUID: row.UUID.String, Name: row.Name.String}
	}
	return ret, nil
}

func (s *SourceStudioStore) Assign(ctx context.Context, collection string, input models.SourceStudioInput) (*models.SourceStudioAssignment, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if input.ExpectedRevision < 0 {
		return nil, models.ErrSourceDefinitionInvalid
	}
	current, err := s.Assignment(ctx, collection)
	if err != nil {
		return nil, err
	}
	var target *models.ArchiveEntity
	var local *int
	if input.StudioUUID != nil {
		if _, err := archiveUUID(*input.StudioUUID); err != nil {
			return nil, models.ErrSourceDefinitionInvalid
		}
		target, err = (&ArchiveEntityStore{}).Resolve(ctx, *input.StudioUUID)
		if err != nil {
			return nil, err
		}
		if target == nil || target.Kind != models.ArchiveStudio || target.State != models.ArchiveEntityActive {
			return nil, models.ErrSourceDefinitionInvalid
		}
		local = target.LocalID
	}
	same := (current.Studio == nil && target == nil) || (current.Studio != nil && target != nil && current.Studio.UUID == target.UUID)
	// A retry of the immediately preceding save is safe. Older requests must
	// not overwrite a later decision, even if the studio has changed back.
	if input.ExpectedRevision != current.Revision {
		if same && input.ExpectedRevision+1 == current.Revision {
			return current, nil
		}
		return nil, models.ErrSourceDefinitionConflict
	}
	if same {
		return current, nil
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_collection_studios(collection_uuid,studio_id,revision) VALUES(?,?,?)
ON CONFLICT(collection_uuid) DO UPDATE SET studio_id=excluded.studio_id,revision=excluded.revision`, current.CollectionUUID, local, current.Revision+1)
	if err != nil {
		return nil, err
	}
	return s.Assignment(ctx, current.CollectionUUID)
}

const sourceStudiosSelect = `SELECT DISTINCT a.uuid,s.name FROM (
SELECT e.post_uuid,b.studio_id FROM source_post_studio_collections e
JOIN source_collection_studios b ON b.collection_uuid=e.collection_uuid
UNION ALL
SELECT i.canonical_uuid,d.studio_id FROM source_post_studios d
JOIN source_post_identities i ON i.post_uuid=d.post_uuid
) e
JOIN studios s ON s.id=e.studio_id
JOIN archive_entities a ON a.studio_id=s.id AND a.state='active'
WHERE `

func (s *SourceStudioStore) ForPost(ctx context.Context, post string) ([]models.SourceStudio, error) {
	ret := []models.SourceStudio{}
	err := dbWrapper.Select(ctx, &ret, sourceStudiosSelect+`e.post_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=?) ORDER BY a.uuid`, post)
	return ret, err
}

func (s *SourceStudioStore) ForGallery(ctx context.Context, gallery string) ([]models.SourceStudio, error) {
	var ids []string
	if err := dbWrapper.Select(ctx, &ids, sourceAlbumGalleryAliasesQuery, gallery); err != nil {
		return nil, err
	}
	if len(ids) > 1024 {
		return nil, models.ErrSourceAlbumLimit
	}
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	ret := []models.SourceStudio{}
	err := dbWrapper.Select(ctx, &ret, sourceStudiosSelect+`e.post_uuid IN (
SELECT i.canonical_uuid FROM post_gallery_links l JOIN source_post_identities i ON i.post_uuid=l.post_uuid
WHERE l.gallery_uuid IN `+getInBinding(len(ids))+`) ORDER BY a.uuid`, args...)
	return ret, err
}
