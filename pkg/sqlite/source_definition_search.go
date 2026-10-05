package sqlite

import (
	"context"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

func sourceDefinitionSearch(filter models.SourceDefinitionFilter, collection bool) (string, []interface{}, error) {
	after, limit, err := sourceDefinitionPage(filter.After, filter.Limit)
	if err != nil || !validAccountText(filter.Query, 256, true) ||
		(filter.State != "" && filter.State != "active" && filter.State != "disabled" && filter.State != "retired") ||
		(filter.Kind != "" && (!collection || !validCollectionKind(filter.Kind))) {
		return "", nil, models.ErrSourceDefinitionInvalid
	}
	where := " WHERE b.uuid>? AND r.revision=b.revision"
	args := []interface{}{after}
	if filter.State != "" {
		where += " AND r.state=?"
		args = append(args, filter.State)
	}
	if filter.Kind != "" {
		where += " AND r.kind=?"
		args = append(args, filter.Kind)
	}
	if filter.Query != "" {
		// User text is literal, including SQL LIKE's wildcard characters.
		query := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(filter.Query) + "%"
		where += " AND (b.uuid=? OR r.label LIKE ? ESCAPE '\\'"
		args = append(args, filter.Query, query)
		if collection {
			where += " OR r.target_url LIKE ? ESCAPE '\\' OR r.path_prefix LIKE ? ESCAPE '\\'"
			args = append(args, query, query)
		} else {
			where += " OR r.server_path LIKE ? ESCAPE '\\'"
			args = append(args, query)
		}
		where += ")"
	}
	return where + " ORDER BY b.uuid LIMIT ?", append(args, limit), nil
}

func (s *SourceCollectionStore) Search(ctx context.Context, filter models.SourceDefinitionFilter) ([]*models.SourceCollection, error) {
	where, args, err := sourceDefinitionSearch(filter, true)
	if err != nil {
		return nil, err
	}
	var rows []sourceCollectionRow
	if err := dbWrapper.Select(ctx, &rows, sourceCollectionSelect+where, args...); err != nil {
		return nil, err
	}
	return resolveCollections(rows), nil
}

func (s *MediaRootStore) Search(ctx context.Context, filter models.SourceDefinitionFilter) ([]*models.MediaRoot, error) {
	where, args, err := sourceDefinitionSearch(filter, false)
	if err != nil {
		return nil, err
	}
	var rows []mediaRootRow
	if err := dbWrapper.Select(ctx, &rows, mediaRootSelect+where, args...); err != nil {
		return nil, err
	}
	ret := make([]*models.MediaRoot, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.resolve())
	}
	return ret, nil
}
