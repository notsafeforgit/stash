package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type FileContentStore struct{}

type mediaContentRow struct {
	UUID      string    `db:"uuid"`
	SHA256    string    `db:"sha256"`
	Size      int64     `db:"size"`
	CreatedAt Timestamp `db:"created_at"`
}

func (r mediaContentRow) resolve() *models.MediaContent {
	return &models.MediaContent{UUID: r.UUID, SHA256: r.SHA256, Size: r.Size, CreatedAt: r.CreatedAt.Timestamp}
}
func (s *FileContentStore) FindContent(ctx context.Context, id string) (*models.MediaContent, error) {
	id, err := archiveUUID(id)
	if err != nil {
		return nil, err
	}
	return findMediaContent(ctx, "uuid", id)
}
func (s *FileContentStore) FindBySHA256(ctx context.Context, digest string) (*models.MediaContent, error) {
	if !archive.ValidSHA256(digest) {
		return nil, errors.New("invalid content SHA-256")
	}
	return findMediaContent(ctx, "sha256", digest)
}
func findMediaContent(ctx context.Context, column, value string) (*models.MediaContent, error) {
	var row mediaContentRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM media_contents WHERE "+column+"=?", value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

type fileContentRow struct {
	FileUUID         string    `db:"file_uuid"`
	Generation       int64     `db:"generation"`
	ContentUUID      string    `db:"content_uuid"`
	SHA256           string    `db:"sha256"`
	Size             int64     `db:"size"`
	ContentCreatedAt Timestamp `db:"content_created_at"`
	RootUUID         string    `db:"root_uuid"`
	RootRevision     int       `db:"root_revision"`
	RelativePath     string    `db:"relative_path"`
	Identity         string    `db:"filesystem_identity"`
	ModTime          string    `db:"mod_time_text"`
	ChangeToken      string    `db:"change_token"`
	VerifiedAt       Timestamp `db:"verified_at"`
}

func (r fileContentRow) resolve() (*models.FileContentVerification, error) {
	mtime, err := time.Parse(time.RFC3339Nano, r.ModTime)
	if err != nil {
		return nil, err
	}
	return &models.FileContentVerification{
		FileUUID: r.FileUUID, Generation: r.Generation,
		Content:  models.MediaContent{UUID: r.ContentUUID, SHA256: r.SHA256, Size: r.Size, CreatedAt: r.ContentCreatedAt.Timestamp},
		RootUUID: r.RootUUID, RootRevision: r.RootRevision, RelativePath: r.RelativePath, VerifiedAt: r.VerifiedAt.Timestamp,
		Snapshot: models.FileSnapshot{Identity: r.Identity, Size: r.Size, ModifiedAt: mtime, ChangeToken: r.ChangeToken},
	}, nil
}

const fileContentSelect = `SELECT v.file_uuid,v.generation,v.content_uuid,c.sha256,c.size,c.created_at AS content_created_at,
v.root_uuid,v.root_revision,v.relative_path,v.filesystem_identity,v.mod_time_text,v.change_token,v.verified_at
FROM file_content_versions v JOIN media_contents c ON c.uuid=v.content_uuid`

func (s *FileContentStore) Current(ctx context.Context, id string) (*models.FileContentVerification, error) {
	id, err := archiveUUID(id)
	if err != nil {
		return nil, err
	}
	var row fileContentRow
	err = dbWrapper.Get(ctx, &row, fileContentSelect+` JOIN archive_entities e ON e.uuid=v.file_uuid
JOIN files f ON f.id=e.file_id AND f.generation=v.generation WHERE v.file_uuid=? AND e.state='active' AND e.kind='file'`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.resolve()
}
func (s *FileContentStore) History(ctx context.Context, id string, after int64, limit int) ([]models.FileContentVerification, error) {
	id, err := archiveUUID(id)
	if err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, errors.New("invalid file generation cursor")
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	var rows []fileContentRow
	if err := dbWrapper.Select(ctx, &rows, fileContentSelect+" WHERE v.file_uuid=? AND v.generation>? ORDER BY v.generation LIMIT ?", id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.FileContentVerification, 0, len(rows))
	for _, row := range rows {
		value, err := row.resolve()
		if err != nil {
			return nil, err
		}
		ret = append(ret, *value)
	}
	return ret, nil
}
func (s *FileContentStore) Locations(ctx context.Context, id, after string, limit int) ([]models.FileContentLocation, error) {
	id, err := archiveUUID(id)
	if err != nil {
		return nil, err
	}
	if after != "" {
		after, err = archiveUUID(after)
		if err != nil {
			return nil, err
		}
	}
	limit, err = ingestPage(after, limit)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		FileUUID   string        `db:"file_uuid"`
		FileID     models.FileID `db:"file_id"`
		Generation int64         `db:"generation"`
	}
	// Start with one content's indexed proofs, never a whole-library file scan.
	if err := dbWrapper.Select(ctx, &rows, `SELECT v.file_uuid,e.file_id,v.generation
FROM file_content_versions v INDEXED BY file_content_versions_content
CROSS JOIN archive_entities e ON e.uuid=v.file_uuid
CROSS JOIN files f ON f.id=e.file_id AND f.generation=v.generation
WHERE v.content_uuid=? AND v.file_uuid>? AND e.state='active' AND e.kind='file'
ORDER BY v.file_uuid LIMIT ?`, id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.FileContentLocation, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, models.FileContentLocation{FileUUID: row.FileUUID, FileID: row.FileID, Generation: row.Generation})
	}
	return ret, nil
}

type contentFileState struct {
	ID         models.FileID `db:"id"`
	Generation int64         `db:"generation"`
	Size       int64         `db:"size"`
	ModTime    Timestamp     `db:"mod_time"`
	Basename   string        `db:"basename"`
	Folder     string        `db:"path"`
}

func (s *FileContentStore) Owners(ctx context.Context, id string) ([]*models.ArchiveEntity, error) {
	id, err := archiveUUID(id)
	if err != nil {
		return nil, err
	}
	return verifiedMediaCandidates(ctx, `FROM archive_entities e`, `e.uuid=? AND e.kind='file' AND e.state='active'`, id)
}

func (s *FileContentStore) MediaCandidates(ctx context.Context, id string) ([]*models.ArchiveEntity, error) {
	id, err := archiveUUID(id)
	if err != nil {
		return nil, err
	}
	return verifiedMediaCandidates(ctx, `FROM file_content_versions v INDEXED BY file_content_versions_content
CROSS JOIN archive_entities e ON e.uuid=v.file_uuid
CROSS JOIN files f ON f.id=e.file_id AND f.generation=v.generation`, `v.content_uuid=? AND e.kind='file' AND e.state='active'`, id)
}

func verifiedMediaCandidates(ctx context.Context, from, where, id string) ([]*models.ArchiveEntity, error) {
	ret := make([]*models.ArchiveEntity, 0, 2)
	for _, relationship := range [][2]string{{scenesFilesTable, "scene_id"}, {imagesFilesTable, "image_id"}} {
		// These table/column names are fixed domain relationships, never inputs.
		// Start at the selected file/content and stop after two distinct owners.
		query := "SELECT DISTINCT m.* " + from + " CROSS JOIN " + relationship[0] + " l ON l.file_id=e.file_id" +
			" CROSS JOIN archive_entities m ON m." + relationship[1] + "=l." + relationship[1] +
			" WHERE " + where + " AND m.state='active' LIMIT ?"
		var rows []archiveEntityRow
		if err := dbWrapper.Select(ctx, &rows, query, id, 2-len(ret)); err != nil {
			return nil, err
		}
		for _, row := range rows {
			ret = append(ret, row.resolve())
		}
		if len(ret) == 2 {
			break
		}
	}
	return ret, nil
}

func contentFile(ctx context.Context, id string, generation int64) (*contentFileState, error) {
	var row contentFileState
	err := dbWrapper.Get(ctx, &row, `SELECT f.id,f.generation,f.size,f.mod_time,f.basename,p.path
FROM archive_entities e JOIN files f ON f.id=e.file_id JOIN folders p ON p.id=f.parent_folder_id
WHERE e.uuid=? AND e.kind='file' AND e.state='active' AND f.generation=? AND f.zip_file_id IS NULL`, id, generation)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrFileGenerationConflict
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *FileContentStore) RecordVerification(ctx context.Context, input models.FileContentInput) (*models.FileContentVerification, error) {
	if _, err := getTx(ctx); err != nil {
		return nil, err
	}
	if writable, _ := ctx.Value(writableKey).(bool); !writable || !txn.HasHooks(ctx) {
		return nil, errors.New("file verification requires a managed write transaction")
	}
	for _, id := range []string{input.FileUUID, input.RootUUID} {
		if normalized, err := archiveUUID(id); err != nil || normalized != id {
			return nil, errors.New("invalid file verification UUID")
		}
	}
	if input.ExpectedGeneration < 1 || input.ExpectedRootRevision < 1 || !archive.ValidSHA256(input.SHA256) || input.Snapshot.Size < 0 || input.Snapshot.ModifiedAt.IsZero() || input.Snapshot.ModifiedAt.Year() < 1 || input.Snapshot.ModifiedAt.Year() > 9999 ||
		!validAccountText(input.Snapshot.Identity, 256, false) || !validAccountText(input.Snapshot.ChangeToken, 256, true) || !archive.ValidRootRelativePath(input.RelativePath, false) {
		return nil, errors.New("invalid file verification")
	}
	state, err := contentFile(ctx, input.FileUUID, input.ExpectedGeneration)
	if err != nil {
		return nil, err
	}
	root, err := (&MediaRootStore{}).Find(ctx, input.RootUUID)
	if err != nil {
		return nil, err
	}
	if root == nil || root.State != "active" || root.Binding == nil || root.Revision != input.ExpectedRootRevision {
		return nil, models.ErrFileGenerationConflict
	}
	if filepath.Join(root.Binding.Path, filepath.FromSlash(input.RelativePath)) != filepath.Join(state.Folder, state.Basename) || state.Size != input.Snapshot.Size || !state.ModTime.Timestamp.Truncate(time.Second).Equal(input.Snapshot.ModifiedAt.Truncate(time.Second)) {
		return nil, models.ErrFileGenerationConflict
	}
	previous, err := s.Current(ctx, input.FileUUID)
	if err != nil {
		return nil, err
	}
	if previous != nil {
		if previous.Content.SHA256 != input.SHA256 || previous.Content.Size != input.Snapshot.Size {
			return nil, models.ErrFileContentConflict
		}
		guardFileContent(ctx, input.FileUUID, input.ExpectedGeneration)
		return previous, nil
	}
	content, err := s.FindBySHA256(ctx, input.SHA256)
	if err != nil {
		return nil, err
	}
	if content != nil && content.Size != input.Snapshot.Size {
		return nil, models.ErrFileContentConflict
	}
	if content == nil {
		id := uuid.NewString()
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO media_contents(uuid,sha256,size) VALUES(?,?,?)", id, input.SHA256, input.Snapshot.Size); err != nil {
			return nil, err
		}
		content, err = s.FindContent(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO file_content_versions(file_uuid,generation,content_uuid,root_uuid,root_revision,relative_path,filesystem_identity,mod_time_text,change_token)
VALUES(?,?,?,?,?,?,?,?,?)`, input.FileUUID, input.ExpectedGeneration, content.UUID, input.RootUUID, root.Revision, input.RelativePath, input.Snapshot.Identity, input.Snapshot.ModifiedAt.UTC().Format(time.RFC3339Nano), input.Snapshot.ChangeToken); err != nil {
		return nil, err
	}
	guardFileContent(ctx, input.FileUUID, input.ExpectedGeneration)
	return s.Current(ctx, input.FileUUID)
}
func guardFileContent(ctx context.Context, id string, generation int64) {
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error { _, err := contentFile(ctx, id, generation); return err })
}
func (s *FileContentStore) Advance(ctx context.Context, id string, expected int64) (int64, error) {
	id, err := archiveUUID(id)
	if err != nil {
		return 0, err
	}
	if expected < 1 {
		return 0, models.ErrFileGenerationConflict
	}
	state, err := contentFile(ctx, id, expected)
	if err != nil {
		return 0, err
	}
	result, err := dbWrapper.Exec(ctx, "UPDATE files SET generation=generation+1 WHERE id=? AND generation=?", state.ID, expected)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n != 1 {
		return 0, models.ErrFileGenerationConflict
	}
	return expected + 1, nil
}
