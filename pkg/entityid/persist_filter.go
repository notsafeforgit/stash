package entityid

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"strconv"
)

// UpgradeFilter is only for one-time imports of legacy configuration. Normal
// API writes validate UUID references before reaching a repository.
func UpgradeFilter(ctx context.Context, repo models.Repository, mode models.FilterMode, filter *models.FilterAST) (*models.FilterAST, error) {
	if filter == nil {
		return nil, nil
	}
	data, err := json.Marshal(filter)
	if err != nil {
		return nil, err
	}
	var node map[string]any
	if err := json.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	mapped, err := MapFilterAST(ModeKind(string(mode)), node, func(kind string, raw any) (any, error) {
		value := fmt.Sprint(raw)
		if id, err := uuid.Parse(value); err == nil && id != uuid.Nil && id.String() == value {
			return value, nil
		}
		id, err := strconv.Atoi(value)
		if err != nil {
			return nil, err
		}
		entity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveEntityKind(kind), id)
		if err != nil {
			return nil, err
		}
		if entity == nil {
			return uuid.NewString(), nil
		}
		return entity.UUID, nil
	})
	if err != nil {
		return nil, err
	}
	data, err = json.Marshal(mapped)
	if err != nil {
		return nil, err
	}
	var result models.FilterAST
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
