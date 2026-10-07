package sqlite

import (
	"context"
	"path"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// DirectoryScopes uses the root/prefix index and current revisions. Historical
// memberships and unbound catalog groups do not grant filesystem ownership.
func (s *SourceCollectionStore) DirectoryScopes(ctx context.Context, root, directory string) ([]*models.SourceCollection, error) {
	if _, err := archiveUUID(root); err != nil || !archive.ValidRootRelativePath(directory, true) {
		return nil, models.ErrSourceDefinitionInvalid
	}
	args := []interface{}{root}
	for current := directory; ; current = path.Dir(current) {
		args = append(args, current)
		if len(args) > 257 {
			return nil, models.ErrSourceDefinitionInvalid
		}
		if current == "." {
			break
		}
	}
	var rows []sourceCollectionRow
	query := sourceCollectionSelect + ` WHERE r.revision=b.revision AND r.root_uuid=? AND r.path_prefix IN ` + getInBinding(len(args)-1) + ` ORDER BY length(r.path_prefix) DESC,b.uuid LIMIT 257`
	if err := dbWrapper.Select(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	if len(rows) > 256 {
		return nil, models.ErrSourceDefinitionInvalid
	}
	ret := make([]*models.SourceCollection, 0, len(rows))
	for _, row := range rows {
		// Whole-root access is used by sources spanning renamed account folders.
		// It is not ownership of every directory. Exclude only folders with
		// direct source-file evidence; do not prune a parent containing separate
		// source and manual subfolders. The location index bounds this lookup.
		if row.PathPrefix == "." && row.Kind != "directory" && row.Kind != "manual_batch" {
			if directory == "." {
				continue
			}
			var observed bool
			if err := dbWrapper.Get(ctx, &observed, `SELECT EXISTS(SELECT 1 FROM source_file_observations INDEXED BY source_file_observation_location
WHERE root_uuid=? AND archive_path IS NULL AND relative_path>=? AND relative_path<? AND collection_uuid=?
AND instr(substr(relative_path,?),'/')=0)`, root, directory+"/", directory+"0", row.UUID, len([]rune(directory))+2); err != nil {
				return nil, err
			}
			if !observed {
				continue
			}
		}
		ret = append(ret, row.resolve())
	}
	return ret, nil
}
