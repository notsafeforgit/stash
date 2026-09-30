package sqlite

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/doug-martin/goqu/v9"
	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

const (
	performerNamesTable = "performer_names"
	// Position zero is the canonical member of the ordered name set.
	performerPrimaryNameSQL   = "(SELECT name FROM performer_names WHERE performer_id = performers.id AND position = 0)"
	performerPrimaryIgnoreSQL = "(SELECT ignore_auto_tag FROM performer_names WHERE performer_id = performers.id AND position = 0)"
)

type performerName struct {
	Name          string `db:"name"`
	IgnoreAutoTag bool   `db:"ignore_auto_tag"`
}

func (qb *PerformerStore) getNames(ctx context.Context, id int) ([]performerName, error) {
	table := goqu.T(performerNamesTable)
	q := dialect.From(table).Select("name", "ignore_auto_tag").
		Where(table.Col(performerIDColumn).Eq(id)).Order(table.Col("position").Asc())
	var ret []performerName
	err := queryFunc(ctx, q, false, func(rows *sqlx.Rows) error {
		var row performerName
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		ret = append(ret, row)
		return nil
	})
	return ret, err
}

func (qb *PerformerStore) replaceNames(ctx context.Context, id int, names []performerName) error {
	if len(names) == 0 {
		return fmt.Errorf("performer %d must have a canonical name", id)
	}
	seen := make(map[string]bool, len(names))
	values := make([][]interface{}, 0, len(names))
	for position, name := range names {
		if seen[name.Name] {
			return fmt.Errorf("performer %d has duplicate name %q", id, name.Name)
		}
		seen[name.Name] = true
		values = append(values, goqu.Vals{id, name.Name, position, name.IgnoreAutoTag})
	}
	table := goqu.T(performerNamesTable)
	if _, err := exec(ctx, dialect.Delete(table).Where(table.Col(performerIDColumn).Eq(id))); err != nil {
		return err
	}
	_, err := exec(ctx, dialect.Insert(table).Cols("performer_id", "name", "position", "ignore_auto_tag").Vals(values...))
	return err
}

func (qb *PerformerStore) updateNames(ctx context.Context, id int, primary models.OptionalString, ignored models.OptionalBool, aliases *models.UpdatePerformerAliases) error {
	if !primary.Set && !ignored.Set && aliases == nil {
		return nil
	}
	names, err := qb.getNames(ctx, id)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("performer %d has no canonical name", id)
	}
	if primary.Set && primary.Value != names[0].Name {
		index := slices.IndexFunc(names, func(n performerName) bool { return n.Name == primary.Value })
		if index >= 0 {
			// Selecting an existing alias moves its policy with it. The previous
			// canonical name takes that alias's position unless explicitly replaced.
			names[0], names[index] = names[index], names[0]
		} else {
			if !strings.EqualFold(primary.Value, names[0].Name) {
				names[0].IgnoreAutoTag = false
			}
			names[0].Name = primary.Value
		}
	}
	if ignored.Set {
		names[0].IgnoreAutoTag = ignored.Value
	}
	if aliases != nil {
		switch aliases.Mode {
		case models.RelationshipUpdateModeSet:
			names = names[:1]
			for _, alias := range aliases.Values {
				names = append(names, performerName{Name: alias.Alias, IgnoreAutoTag: alias.IgnoreAutoTag})
			}
		case models.RelationshipUpdateModeAdd:
			for _, alias := range aliases.Values {
				index := slices.IndexFunc(names, func(n performerName) bool { return n.Name == alias.Alias })
				if index == 0 {
					return fmt.Errorf("canonical name %q cannot also be an alias", alias.Alias)
				}
				name := performerName{Name: alias.Alias, IgnoreAutoTag: alias.IgnoreAutoTag}
				if index < 0 {
					names = append(names, name)
				} else {
					names[index] = name
				}
			}
		case models.RelationshipUpdateModeRemove:
			for _, alias := range aliases.Values {
				index := slices.IndexFunc(names, func(n performerName) bool { return n.Name == alias.Alias })
				if index > 0 {
					names = slices.Delete(names, index, index+1)
				}
			}
		default:
			return fmt.Errorf("unsupported performer name update mode %q", aliases.Mode)
		}
	}
	return qb.replaceNames(ctx, id, names)
}

func (qb *PerformerStore) GetPerformerAliases(ctx context.Context, performerID int) ([]models.PerformerAlias, error) {
	names, err := qb.getNames(ctx, performerID)
	if err != nil {
		return nil, err
	}
	var ret []models.PerformerAlias
	for index, name := range names {
		if index != 0 {
			ret = append(ret, models.PerformerAlias{Alias: name.Name, IgnoreAutoTag: name.IgnoreAutoTag})
		}
	}
	return ret, nil
}
