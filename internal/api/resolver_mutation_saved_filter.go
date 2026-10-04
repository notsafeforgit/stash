package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

func (r *mutationResolver) SaveFilter(ctx context.Context, input SaveFilterInput) (ret *models.SavedFilter, err error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, errors.New("name must be non-empty")
	}

	var id *int
	if input.ID != nil {
		idv, err := strconv.Atoi(*input.ID)
		if err != nil {
			return nil, fmt.Errorf("converting id: %w", err)
		}
		id = &idv
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		qb := r.repository.SavedFilter

		f := models.SavedFilter{
			Mode:       input.Mode,
			Name:       strings.TrimSpace(input.Name),
			FindFilter: input.FindFilter,
			UIOptions:  input.UIOptions,
		}

		if input.FilterAst != nil {
			normalized, err := input.FilterAst.Normalize()
			if err != nil {
				return fmt.Errorf("invalid filter AST: %w", err)
			}
			f.FilterAST = normalized
		}

		if id == nil {
			err = qb.Create(ctx, &f)
			ret = &f
		} else {
			f.ID = *id
			err = qb.Update(ctx, &f)
			ret = &f
		}

		return err
	}); err != nil {
		return nil, err
	}
	return ret, err
}

func (r *mutationResolver) DestroySavedFilter(ctx context.Context, input DestroyFilterInput) (bool, error) {
	id, err := strconv.Atoi(input.ID)
	if err != nil {
		return false, fmt.Errorf("converting id: %w", err)
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		return r.repository.SavedFilter.Destroy(ctx, id)
	}); err != nil {
		return false, err
	}

	return true, nil
}
