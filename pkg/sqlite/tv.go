package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

// QueryTV composes the existing AST/search queries before pagination. Testing
// overlap against the complete marker result prevents a scene appearing just
// because its matching marker happens to be on a later page.
func (qb *SceneMarkerStore) QueryTV(ctx context.Context, markerAST, sceneAST *models.FilterAST, findFilter *models.FindFilterType, sceneSearch *string) ([]*models.TVFeedItem, int, error) {
	if findFilter == nil {
		findFilter = &models.FindFilterType{}
	}
	sort := findFilter.GetSort("title")
	if err := sceneMarkerSortOptions.validateSort(sort); err != nil {
		return nil, 0, err
	}
	markers, err := qb.makeASTQuery(ctx, markerAST, &models.FindFilterType{Q: findFilter.Q})
	if err != nil {
		return nil, 0, err
	}
	sceneStore := qb.repo.Scene
	scenes, err := sceneStore.makeASTQuery(ctx, sceneAST, &models.FindFilterType{Q: sceneSearch})
	if err != nil {
		return nil, 0, err
	}
	// Even/odd feed IDs keep random order stable and distinguish a marker and
	// a scene with the same database key. These keys never leave the repository.
	with := fmt.Sprintf(`WITH matching_markers AS (%s), matching_scenes AS (%s),
		tv_items AS (
		  SELECT m.id * 2 AS id, m.scene_id, m.id AS marker_id,
		    COALESCE(NULLIF(m.title, ''), t.name) AS title,
		    m.created_at, m.updated_at, s.updated_at AS scenes_updated_at,
		    m.seconds, m.end_seconds - m.seconds AS duration
		  FROM matching_markers mm JOIN scene_markers m ON m.id = mm.id
		  JOIN scenes s ON s.id = m.scene_id LEFT JOIN tags t ON t.id = m.primary_tag_id
		  UNION ALL
		  SELECT s.id * 2 + 1, s.id, NULL, s.title, s.created_at, s.updated_at, s.updated_at,
		    0, vf.duration
		  FROM matching_scenes ms JOIN scenes s ON s.id = ms.id
		  LEFT JOIN scenes_files sf ON sf.scene_id = s.id AND sf.[primary] = 1
		  LEFT JOIN video_files vf ON vf.file_id = sf.file_id
		  WHERE NOT EXISTS (
		    SELECT 1 FROM matching_markers mm JOIN scene_markers m ON m.id = mm.id
		    WHERE m.scene_id = s.id
		  )
		) `, markers.toSQL(false), scenes.toSQL(false))
	args := append(markers.allArgs(), scenes.allArgs()...)
	var total int
	if err := dbWrapper.Get(ctx, &total, with+"SELECT COUNT(*) FROM tv_items", args...); err != nil {
		return nil, 0, err
	}
	query := with + "SELECT scene_id, marker_id FROM tv_items" +
		getSort(sort, findFilter.GetDirection(), "tv_items") +
		", tv_items.scene_id ASC, tv_items.seconds ASC, tv_items.id ASC" + getPagination(findFilter)
	var rows []struct {
		SceneID  int           `db:"scene_id"`
		MarkerID sql.NullInt64 `db:"marker_id"`
	}
	if err := dbWrapper.Select(ctx, &rows, query, args...); err != nil {
		return nil, 0, err
	}
	sceneIDs := make([]int, len(rows))
	var markerIDs []int
	for i, row := range rows {
		sceneIDs[i] = row.SceneID
		if row.MarkerID.Valid {
			markerIDs = append(markerIDs, int(row.MarkerID.Int64))
		}
	}
	parentScenes, err := sceneStore.FindByIDs(ctx, sceneIDs)
	if err != nil {
		return nil, 0, err
	}
	matchingMarkers, err := qb.FindMany(ctx, markerIDs)
	if err != nil {
		return nil, 0, err
	}
	byScene := make(map[int]*models.Scene, len(parentScenes))
	for _, scene := range parentScenes {
		byScene[scene.ID] = scene
	}
	byMarker := make(map[int]*models.SceneMarker, len(matchingMarkers))
	for _, marker := range matchingMarkers {
		byMarker[marker.ID] = marker
	}
	items := make([]*models.TVFeedItem, len(rows))
	for i, row := range rows {
		items[i] = &models.TVFeedItem{Scene: byScene[row.SceneID], Marker: byMarker[int(row.MarkerID.Int64)]}
	}
	return items, total, nil
}
