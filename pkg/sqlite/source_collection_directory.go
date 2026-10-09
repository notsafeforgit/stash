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
	query := sourceCollectionSelect + ` WHERE r.revision=b.revision AND r.root_uuid=? AND r.path_prefix IN ` + getInBinding(len(args)-1)
	// Root-wide access permits account folder renames; it does not establish
	// ownership of every folder. Filter it before the per-directory bound so a
	// large source list cannot prevent unrelated manual intake. The uncorrelated
	// subquery reads the selected directory's file evidence once through its index.
	query += ` AND NOT EXISTS(SELECT 1 FROM source_collection_aliases a WHERE a.alias_uuid=b.uuid) AND (r.path_prefix!='.' OR r.kind IN ('directory','manual_batch')`
	if directory != "." {
		query += ` OR b.uuid IN (SELECT DISTINCT coalesce((SELECT source_uuid FROM source_collection_aliases WHERE alias_uuid=o.collection_uuid),o.collection_uuid) FROM source_file_observations o INDEXED BY source_file_observation_location
WHERE root_uuid=? AND archive_path IS NULL AND relative_path>=? AND relative_path<?
AND instr(substr(relative_path,?),'/')=0)`
		args = append(args, root, directory+"/", directory+"0", len([]rune(directory))+2)
	}
	query += `) ORDER BY length(r.path_prefix) DESC,b.uuid LIMIT 257`
	var rows []sourceCollectionRow
	if err := dbWrapper.Select(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	if len(rows) > 256 {
		return nil, models.ErrSourceDefinitionInvalid
	}
	ret := make([]*models.SourceCollection, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.resolve())
	}
	return ret, nil
}
