package sqlite

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func metadataSampleScope(ctx context.Context, scope models.MetadataPolicySampleScope) (*models.SourceCollection, *models.ArchiveEntity, error) {
	if scope.CollectionRevision < 1 || sourceFileIDs(&scope.CollectionUUID, &scope.EntityUUID) != nil {
		return nil, nil, models.ErrMetadataPolicyInvalid
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, scope.CollectionUUID)
	if err != nil {
		return nil, nil, err
	}
	entity, err := (&ArchiveEntityStore{}).Find(ctx, scope.EntityUUID)
	if err != nil {
		return nil, nil, err
	}
	if collection == nil || collection.Revision != scope.CollectionRevision || collection.State == "retired" ||
		!archiveMedia(entity) || entity.State != models.ArchiveEntityActive || entity.LocalID == nil {
		return nil, nil, models.ErrMetadataPolicyConflict
	}
	return collection, entity, nil
}

func (s *MetadataPolicyStore) SampleFiles(ctx context.Context, scope models.MetadataPolicySampleScope, after string, limit int) ([]models.MetadataPolicySampleFile, error) {
	collection, entity, err := metadataSampleScope(ctx, scope)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil || (after != "" && sourceFileIDs(&after) != nil) {
		return nil, models.ErrMetadataPolicyInvalid
	}
	ret := []models.MetadataPolicySampleFile{}
	if collection.RootUUID == nil {
		return ret, nil
	}
	root, err := (&MediaRootStore{}).Find(ctx, *collection.RootUUID)
	if err != nil {
		return nil, err
	}
	if root == nil || root.State != "active" || root.Binding == nil {
		return ret, nil
	}
	directory := filepath.Join(root.Binding.Path, filepath.FromSlash(collection.PathPrefix))
	prefix := directory
	if prefix[len(prefix)-1] != filepath.Separator {
		prefix += string(filepath.Separator)
	}
	table, column, err := metadataFileOwnerTable(entity.Kind)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		UUID      string `db:"uuid"`
		Directory string `db:"directory"`
		Basename  string `db:"basename"`
	}
	// The owner relation selects one scene/image first. Do not enumerate the
	// library, and do not silently use ZIP members as physical sample files.
	err = dbWrapper.Select(ctx, &rows, `SELECT f.uuid, folder.path AS directory, file.basename
FROM `+table+` owner JOIN archive_entities f ON f.file_id=owner.file_id AND f.state='active'
JOIN files file ON file.id=owner.file_id JOIN folders folder ON folder.id=file.parent_folder_id
WHERE owner.`+column+`=? AND f.uuid>? AND file.zip_file_id IS NULL
AND (folder.path=? OR substr(folder.path,1,length(?))=?) ORDER BY f.uuid LIMIT ?`,
		*entity.LocalID, after, directory, prefix, prefix, limit)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		relative, err := filepath.Rel(root.Binding.Path, filepath.Join(row.Directory, row.Basename))
		if err != nil || !archive.ValidRootRelativePath(filepath.ToSlash(relative), false) {
			return nil, models.ErrMetadataPolicyConflict
		}
		ret = append(ret, models.MetadataPolicySampleFile{FileUUID: row.UUID, RelativePath: filepath.ToSlash(relative)})
	}
	return ret, nil
}

func (s *MetadataPolicyStore) SampleSources(ctx context.Context, scope models.MetadataPolicySampleScope, after *models.MetadataPolicySourceCursor, limit int) ([]models.MetadataPolicySampleSource, error) {
	_, entity, err := metadataSampleScope(ctx, scope)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrMetadataPolicyInvalid
	}
	cursor := models.MetadataPolicySourceCursor{}
	if after != nil {
		cursor = *after
		if sourceFileIDs(&cursor.CaptureUUID, &cursor.AttachmentUUID) != nil {
			return nil, models.ErrMetadataPolicyInvalid
		}
	}
	// Retained choices can name a merged identity. Reverse redirects are
	// indexed and bounded; never truncate them into a falsely complete result.
	var identities []string
	err = dbWrapper.Select(ctx, &identities, `WITH RECURSIVE identities(uuid) AS (
SELECT ? UNION SELECT e.uuid FROM archive_entities e JOIN identities i ON e.redirect_to=i.uuid LIMIT 1025
) SELECT uuid FROM identities`, entity.UUID)
	if err != nil {
		return nil, err
	}
	if len(identities) > 1024 {
		return nil, fmt.Errorf("%w: sample has more than 1024 merged identities", models.ErrMetadataPolicyInvalid)
	}
	args := make([]interface{}, 0, len(identities)+5)
	for _, id := range identities {
		args = append(args, id)
	}
	args = append(args, scope.CollectionUUID, scope.CollectionRevision, cursor.CaptureUUID, cursor.AttachmentUUID, limit)
	var rows []struct {
		CaptureUUID    string        `db:"capture_uuid"`
		AttachmentUUID string        `db:"attachment_uuid"`
		PostUUID       string        `db:"post_uuid"`
		Title          string        `db:"title"`
		Platform       string        `db:"platform"`
		Origin         string        `db:"origin"`
		CapturedAt     NullTimestamp `db:"captured_at"`
	}
	// Only current linked attachments, evidenced capture membership, and the
	// reviewed collection revision are eligible. DISTINCT collapses repeated
	// attachment slots without combining captures from different source times.
	err = dbWrapper.Select(ctx, &rows, `SELECT DISTINCT c.uuid AS capture_uuid,d.attachment_uuid,c.post_uuid,
COALESCE(json_extract(r.metadata,'$.title'),'') AS title,c.platform,c.origin,c.captured_at
FROM attachment_media_decisions d
JOIN attachment_media_links l ON l.attachment_uuid=d.attachment_uuid AND l.decision_uuid=d.uuid
JOIN source_attachment_entries e ON e.attachment_uuid=d.attachment_uuid
JOIN source_capture_attachment_manifests m ON m.manifest_uuid=e.manifest_uuid
JOIN source_captures c ON c.uuid=m.capture_uuid
JOIN source_post_revisions r ON r.uuid=c.revision_uuid
JOIN source_posts p ON p.uuid=c.post_uuid AND p.state='active'
JOIN source_collection_captures cc ON cc.capture_uuid=c.uuid
WHERE d.media_uuid IN `+getInBinding(len(identities))+` AND d.state='linked'
AND cc.collection_uuid=? AND cc.collection_revision=?
AND (c.uuid,d.attachment_uuid)>(?,?) ORDER BY c.uuid,d.attachment_uuid LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	ret := make([]models.MetadataPolicySampleSource, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, models.MetadataPolicySampleSource{MetadataPolicySourceCursor: models.MetadataPolicySourceCursor{CaptureUUID: row.CaptureUUID, AttachmentUUID: row.AttachmentUUID},
			PostUUID: row.PostUUID, Title: row.Title, Platform: row.Platform, Origin: row.Origin, CapturedAt: row.CapturedAt.TimePtr()})
	}
	return ret, nil
}
