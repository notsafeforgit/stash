package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

func (r *savedFilterResolver) FilterAst(ctx context.Context, obj *models.SavedFilter) (map[string]any, error) {
	if obj.FilterAST == nil {
		return nil, nil
	}

	// *models.FilterAST -> generic Map via its JSON form
	encoded, err := json.Marshal(obj.FilterAST)
	if err != nil {
		return nil, fmt.Errorf("encoding filter AST: %w", err)
	}

	var ret map[string]any
	if err := json.Unmarshal(encoded, &ret); err != nil {
		return nil, fmt.Errorf("decoding filter AST: %w", err)
	}

	return ret, nil
}
