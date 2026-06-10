package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

func (r *savedFilterResolver) Filter(ctx context.Context, obj *models.SavedFilter) (string, error) {
	return "", nil
}

// ObjectFilter is a v2.5 compatibility view flattened from the canonical
// filter AST. The flat shape can only express an AND of one criterion per
// type, so complex filters (OR/nesting/repeated fields) render partially.
func (r *savedFilterResolver) ObjectFilter(ctx context.Context, obj *models.SavedFilter) (map[string]any, error) {
	if obj.FilterAST != nil {
		flat, _ := obj.FilterAST.FlatObjectFilter()
		return flat, nil
	}

	// fallback for rows the schema migration could not convert
	if obj.ObjectFilter == nil {
		return nil, nil
	}
	ret := make(map[string]any, len(obj.ObjectFilter))
	for k, v := range obj.ObjectFilter {
		// the transitional compact-AST key is not part of the v2.5 shape
		if k == models.LegacyFilterASTKey {
			continue
		}
		ret[k] = v
	}
	return ret, nil
}

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
