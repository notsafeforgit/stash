package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

func (qb *PerformerStore) CountMany(ctx context.Context, keys []models.EntityCountKey) ([]int, error) {
	return entityCounts(ctx, keys, false)
}

func (qb *StudioStore) CountMany(ctx context.Context, keys []models.EntityCountKey) ([]int, error) {
	return entityCounts(ctx, keys, true)
}

// Group by requested aggregate, then count every requested entity in one query.
// Joining independent relationships together would multiply rows (and counters).
func entityCounts(ctx context.Context, keys []models.EntityCountKey, studio bool) ([]int, error) {
	type scope struct {
		field models.EntityCountField
		depth int
	}
	groups := make(map[scope][]int)
	for _, key := range keys {
		s := scope{key.Field, key.Depth}
		groups[s] = append(groups[s], key.ID)
	}
	values := make(map[models.EntityCountKey]int)
	for s, ids := range groups {
		encoded, err := json.Marshal(ids)
		if err != nil {
			return nil, err
		}
		prefix := "WITH selected(id) AS (SELECT DISTINCT value FROM json_each(?)) "
		query, err := performerCountSQL(s.field)
		args := []interface{}{string(encoded)}
		if studio {
			prefix = `WITH RECURSIVE selected(id) AS (SELECT DISTINCT value FROM json_each(?)),
hierarchy(root, id, depth, path) AS (
 SELECT id, id, 0, '/' || id || '/' FROM selected
 UNION ALL
 SELECT h.root, s.id, h.depth + 1, h.path || s.id || '/'
 FROM hierarchy h JOIN studios s ON s.parent_id = h.id
 WHERE (? = -1 OR h.depth < ?) AND instr(h.path, '/' || s.id || '/') = 0
) `
			args = append(args, s.depth, s.depth)
			query, err = studioCountSQL(s.field)
		}
		if err != nil {
			return nil, err
		}
		rows, err := dbWrapper.Queryx(ctx, prefix+query, args...)
		if err != nil {
			return nil, err
		}
		err = func() error {
			defer rows.Close()
			for rows.Next() {
				var id, count int
				if err := rows.Scan(&id, &count); err != nil {
					return err
				}
				values[models.EntityCountKey{ID: id, Depth: s.depth, Field: s.field}] = count
			}
			return rows.Err()
		}()
		if err != nil {
			return nil, err
		}
	}
	ret := make([]int, len(keys))
	for i, key := range keys {
		ret[i] = values[key]
	}
	return ret, nil
}

func performerCountSQL(field models.EntityCountField) (string, error) {
	switch field {
	case models.EntityCountScenes, models.EntityCountImages, models.EntityCountGalleries:
		return "SELECT performer_id, count(*) FROM performers_" + string(field) + " JOIN selected ON selected.id = performer_id GROUP BY performer_id", nil
	case models.EntityCountGroups:
		return `SELECT performer_id, count(DISTINCT group_id) FROM selected
 JOIN performers_scenes ON performer_id = selected.id
 JOIN groups_scenes USING(scene_id) GROUP BY performer_id`, nil
	case models.EntityCountPerformers:
		var parts []string
		for _, pair := range []struct{ table, id string }{{"performers_scenes", "scene_id"}, {"performers_images", "image_id"}, {"performers_galleries", "gallery_id"}} {
			parts = append(parts, "SELECT selected.id, b.performer_id FROM selected JOIN "+pair.table+" a ON a.performer_id = selected.id JOIN "+pair.table+" b ON b."+pair.id+" = a."+pair.id+" WHERE b.performer_id != selected.id")
		}
		return "SELECT id, count(*) FROM (" + strings.Join(parts, " UNION ") + ") GROUP BY id", nil
	case models.EntityCountO:
		return `SELECT id, sum(n) FROM (
 SELECT selected.id, count(*) n FROM selected JOIN performers_scenes ON performer_id = selected.id
 JOIN scenes_o_dates USING(scene_id) GROUP BY selected.id
 UNION ALL
 SELECT selected.id, coalesce(sum(o_counter), 0) FROM selected JOIN performers_images ON performer_id = selected.id
 JOIN images ON images.id = image_id GROUP BY selected.id
) GROUP BY id`, nil
	case models.EntityHasImage:
		return "SELECT performers.id, image_blob IS NOT NULL FROM performers JOIN selected ON selected.id = performers.id", nil
	default:
		return "", fmt.Errorf("unsupported performer aggregate %q", field)
	}
}

func studioCountSQL(field models.EntityCountField) (string, error) {
	switch field {
	case models.EntityCountScenes, models.EntityCountImages, models.EntityCountGalleries, models.EntityCountGroups:
		return "SELECT h.root, count(*) FROM hierarchy h JOIN " + string(field) + " m ON m.studio_id = h.id GROUP BY h.root", nil
	case models.EntityCountPerformers:
		var parts []string
		for _, pair := range []struct{ table, id string }{{"scenes", "scene_id"}, {"images", "image_id"}, {"galleries", "gallery_id"}} {
			parts = append(parts, "SELECT h.root, p.performer_id FROM hierarchy h JOIN "+pair.table+" m ON m.studio_id = h.id JOIN performers_"+pair.table+" p ON p."+pair.id+" = m.id")
		}
		return "SELECT root, count(*) FROM (" + strings.Join(parts, " UNION ") + ") GROUP BY root", nil
	case models.EntityCountMarkers:
		return "SELECT h.root, count(*) FROM hierarchy h JOIN scenes s ON s.studio_id = h.id JOIN scene_markers m ON m.scene_id = s.id GROUP BY h.root", nil
	case models.EntityCountO:
		return `SELECT root, sum(n) FROM (
 SELECT h.root, count(*) n FROM hierarchy h JOIN scenes s ON s.studio_id = h.id
 JOIN scenes_o_dates o ON o.scene_id = s.id GROUP BY h.root
 UNION ALL
 SELECT h.root, coalesce(sum(o_counter), 0) FROM hierarchy h JOIN images i ON i.studio_id = h.id GROUP BY h.root
) GROUP BY root`, nil
	case models.EntityHasImage:
		return "SELECT studios.id, image_blob IS NOT NULL FROM studios JOIN selected ON selected.id = studios.id", nil
	default:
		return "", fmt.Errorf("unsupported studio aggregate %q", field)
	}
}
