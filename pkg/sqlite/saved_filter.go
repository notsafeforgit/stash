package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/doug-martin/goqu/v9"
	"github.com/doug-martin/goqu/v9/exp"
	"github.com/jmoiron/sqlx"

	"github.com/stashapp/stash/pkg/models"
)

const (
	savedFilterTable       = "saved_filters"
	savedFilterDefaultName = ""
)

type savedFilterRow struct {
	ID         int               `db:"id" goqu:"skipinsert"`
	Mode       models.FilterMode `db:"mode"`
	Name       string            `db:"name"`
	FindFilter string            `db:"find_filter"`
	FilterAST  string            `db:"filter_ast"`
	UIOptions  string            `db:"ui_options"`
}

func encodeJSONOrEmpty(v interface{}) (string, error) {
	if v == nil {
		return "", nil
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func decodeJSON(s string, v interface{}) error {
	if s == "" {
		return nil
	}
	return json.Unmarshal([]byte(s), v)
}

func (r *savedFilterRow) fromSavedFilter(o models.SavedFilter) error {
	r.ID, r.Mode, r.Name = o.ID, o.Mode, o.Name
	var err error
	r.FindFilter, err = encodeJSONOrEmpty(o.FindFilter)
	if err != nil {
		return fmt.Errorf("encoding find filter: %w", err)
	}
	ast := o.FilterAST
	if ast == nil && len(o.ObjectFilter) != 0 {
		// Transitional callers and historical import files are converted at the
		// boundary; only the canonical AST is persisted.
		ast, err = models.FilterASTFromLegacySavedFilter(o.ObjectFilter)
		if err != nil {
			return fmt.Errorf("converting imported filter: %w", err)
		}
	}
	if ast != nil {
		ast, err = ast.Normalize()
		if err != nil {
			return fmt.Errorf("invalid saved-filter AST: %w", err)
		}
		r.FilterAST, err = encodeJSONOrEmpty(ast)
		if err != nil {
			return fmt.Errorf("encoding saved-filter AST: %w", err)
		}
	}
	r.UIOptions, err = encodeJSONOrEmpty(o.UIOptions)
	if err != nil {
		return fmt.Errorf("encoding filter UI options: %w", err)
	}
	return nil
}

func (r *savedFilterRow) resolve() (*models.SavedFilter, error) {
	ret := &models.SavedFilter{ID: r.ID, Mode: r.Mode, Name: r.Name}
	if err := decodeJSON(r.FindFilter, &ret.FindFilter); err != nil {
		return nil, fmt.Errorf("decoding find filter: %w", err)
	}
	if err := decodeJSON(r.FilterAST, &ret.FilterAST); err != nil {
		return nil, fmt.Errorf("decoding filter AST: %w", err)
	}
	if ret.FilterAST != nil {
		if err := ret.FilterAST.Validate(); err != nil {
			return nil, fmt.Errorf("invalid stored filter AST: %w", err)
		}
	}
	if err := decodeJSON(r.UIOptions, &ret.UIOptions); err != nil {
		return nil, fmt.Errorf("decoding filter UI options: %w", err)
	}
	return ret, nil
}

type SavedFilterStore struct {
	repository
	tableMgr *table
}

func NewSavedFilterStore() *SavedFilterStore {
	return &SavedFilterStore{
		repository: repository{
			tableName: savedFilterTable,
			idColumn:  idColumn,
		},
		tableMgr: savedFilterTableMgr,
	}
}

func (qb *SavedFilterStore) table() exp.IdentifierExpression {
	return qb.tableMgr.table
}

func (qb *SavedFilterStore) selectDataset() *goqu.SelectDataset {
	return dialect.From(qb.table()).Select(qb.table().All())
}

func (qb *SavedFilterStore) Create(ctx context.Context, newObject *models.SavedFilter) error {
	var r savedFilterRow
	if err := r.fromSavedFilter(*newObject); err != nil {
		return err
	}

	id, err := qb.tableMgr.insertID(ctx, r)
	if err != nil {
		return err
	}

	updated, err := qb.Find(ctx, id)
	if err != nil {
		return fmt.Errorf("finding after create: %w", err)
	}

	*newObject = *updated

	return nil
}

func (qb *SavedFilterStore) Update(ctx context.Context, updatedObject *models.SavedFilter) error {
	var r savedFilterRow
	if err := r.fromSavedFilter(*updatedObject); err != nil {
		return err
	}

	if err := qb.tableMgr.updateByID(ctx, updatedObject.ID, r); err != nil {
		return err
	}

	return nil
}

func (qb *SavedFilterStore) Destroy(ctx context.Context, id int) error {
	return qb.destroyExisting(ctx, []int{id})
}

// returns nil, nil if not found
func (qb *SavedFilterStore) Find(ctx context.Context, id int) (*models.SavedFilter, error) {
	ret, err := qb.find(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

func (qb *SavedFilterStore) FindMany(ctx context.Context, ids []int, ignoreNotFound bool) ([]*models.SavedFilter, error) {
	ret := make([]*models.SavedFilter, len(ids))

	table := qb.table()
	q := qb.selectDataset().Prepared(true).Where(table.Col(idColumn).In(ids))
	unsorted, err := qb.getMany(ctx, q)
	if err != nil {
		return nil, err
	}

	for _, s := range unsorted {
		i := slices.Index(ids, s.ID)
		ret[i] = s
	}

	if !ignoreNotFound {
		for i := range ret {
			if ret[i] == nil {
				return nil, fmt.Errorf("filter with id %d not found", ids[i])
			}
		}
	}

	return ret, nil
}

// returns nil, sql.ErrNoRows if not found
func (qb *SavedFilterStore) find(ctx context.Context, id int) (*models.SavedFilter, error) {
	q := qb.selectDataset().Where(qb.tableMgr.byID(id))

	ret, err := qb.get(ctx, q)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (qb *SavedFilterStore) get(ctx context.Context, q *goqu.SelectDataset) (*models.SavedFilter, error) {
	ret, err := qb.getMany(ctx, q)
	if err != nil {
		return nil, err
	}

	if len(ret) == 0 {
		return nil, sql.ErrNoRows
	}

	return ret[0], nil
}

func (qb *SavedFilterStore) getMany(ctx context.Context, q *goqu.SelectDataset) ([]*models.SavedFilter, error) {
	const single = false
	var ret []*models.SavedFilter
	if err := queryFunc(ctx, q, single, func(r *sqlx.Rows) error {
		var f savedFilterRow
		if err := r.StructScan(&f); err != nil {
			return err
		}

		s, err := f.resolve()
		if err != nil {
			return fmt.Errorf("reading saved filter %d: %w", f.ID, err)
		}

		ret = append(ret, s)
		return nil
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (qb *SavedFilterStore) FindByMode(ctx context.Context, mode models.FilterMode) ([]*models.SavedFilter, error) {
	// SELECT * FROM %s WHERE mode = ? AND name != ? ORDER BY name ASC
	table := qb.table()

	// TODO - querying on groups needs to include movies
	// remove this when we migrate to remove the movies filter mode in the database
	var whereClause exp.Expression

	if mode == models.FilterModeGroups || mode == models.FilterModeMovies {
		whereClause = goqu.Or(
			table.Col("mode").Eq(models.FilterModeGroups),
			table.Col("mode").Eq(models.FilterModeMovies),
		)
	} else {
		whereClause = table.Col("mode").Eq(mode)
	}

	sq := qb.selectDataset().Prepared(true).Where(whereClause).Order(table.Col("name").Asc())
	ret, err := qb.getMany(ctx, sq)

	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (qb *SavedFilterStore) All(ctx context.Context) ([]*models.SavedFilter, error) {
	return qb.getMany(ctx, qb.selectDataset())
}
