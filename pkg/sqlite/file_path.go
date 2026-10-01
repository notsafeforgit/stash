package sqlite

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/stashapp/stash/pkg/models"
)

type FilePathStore struct{}

func (s *FilePathStore) Snapshot(ctx context.Context, path string, caseSensitive bool) (*models.FilePathFence, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == string(filepath.Separator) || !validAccountText(path, 32768, false) {
		return nil, errors.New("invalid local file path")
	}
	query := "SELECT coalesce(sum(revision),0) FROM file_path_fences WHERE folder_path=? AND basename=?"
	if !caseSensitive {
		// Sum all spelling variants: max() could miss a new removal whose count
		// is lower than another variant's. Use the same collation as File.FindByPath.
		query = "SELECT coalesce(sum(revision),0) FROM file_path_fences WHERE folder_path=? COLLATE NOCASE AND basename=? COLLATE NOCASE"
	}
	ret := &models.FilePathFence{Path: path, CaseSensitive: caseSensitive}
	if err := dbWrapper.Get(ctx, &ret.Revision, query, filepath.Dir(path), filepath.Base(path)); err != nil {
		return nil, err
	}
	return ret, nil
}

func (s *FilePathStore) Check(ctx context.Context, expected models.FilePathFence) error {
	if expected.Revision < 0 {
		return models.ErrFilePathChanged
	}
	current, err := s.Snapshot(ctx, expected.Path, expected.CaseSensitive)
	if err != nil {
		return err
	}
	if current.Revision != expected.Revision {
		return models.ErrFilePathChanged
	}
	return nil
}
