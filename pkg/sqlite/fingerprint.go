package sqlite

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"github.com/doug-martin/goqu/v9"
	"github.com/doug-martin/goqu/v9/exp"
	"github.com/stashapp/stash/pkg/models"
	"gopkg.in/guregu/null.v4"
)

const (
	fingerprintTable = "files_fingerprints"
)

type fingerprintQueryRow struct {
	Type        null.String `db:"fingerprint_type"`
	Fingerprint interface{} `db:"fingerprint"`
}

func (r fingerprintQueryRow) valid() bool {
	return r.Type.Valid
}

func (r *fingerprintQueryRow) resolve() models.Fingerprint {
	return models.Fingerprint{
		Type:        r.Type.String,
		Fingerprint: r.Fingerprint,
	}
}

type fingerprintQueryBuilder struct {
	repository

	tableMgr *table
}

var FingerprintReaderWriter = &fingerprintQueryBuilder{
	repository: repository{
		tableName: fingerprintTable,
		idColumn:  fileIDColumn,
	},

	tableMgr: fingerprintTableMgr,
}

func (qb *fingerprintQueryBuilder) insert(ctx context.Context, fileID models.FileID, f models.Fingerprint) error {
	table := qb.table()
	q := dialect.Insert(table).Cols(fileIDColumn, "type", "fingerprint").Vals(
		goqu.Vals{fileID, f.Type, f.Fingerprint},
	)
	_, err := exec(ctx, q)
	if err != nil {
		return fmt.Errorf("inserting into %s: %w", table.GetTable(), err)
	}

	return nil
}

func (qb *fingerprintQueryBuilder) insertJoins(ctx context.Context, fileID models.FileID, f []models.Fingerprint) error {
	for _, ff := range f {
		if err := qb.insert(ctx, fileID, ff); err != nil {
			return err
		}
	}

	return nil
}

func (qb *fingerprintQueryBuilder) upsertJoins(ctx context.Context, fileID models.FileID, f []models.Fingerprint) error {
	types := make([]string, len(f))
	for i, ff := range f {
		types[i] = ff.Type
	}

	if len(types) == 0 {
		return nil
	}
	return qb.reconcileJoins(ctx, fileID, f, types)
}

func (qb *fingerprintQueryBuilder) replaceJoins(ctx context.Context, fileID models.FileID, f []models.Fingerprint) error {
	return qb.reconcileJoins(ctx, fileID, f, nil)
}

// Preserve unchanged fingerprints. Delete/reinsert would create false file
// generations on every metadata rescan, even when the bytes did not change.
func (qb *fingerprintQueryBuilder) reconcileJoins(ctx context.Context, fileID models.FileID, next []models.Fingerprint, types []string) error {
	var rows []struct {
		Type        string      `db:"type"`
		Fingerprint interface{} `db:"fingerprint"`
	}
	if err := dbWrapper.Select(ctx, &rows, "SELECT type,fingerprint FROM files_fingerprints WHERE file_id=?", fileID); err != nil {
		return err
	}
	contains := func(list []models.Fingerprint, value models.Fingerprint) bool {
		return slices.ContainsFunc(list, func(f models.Fingerprint) bool {
			return f.Type == value.Type && reflect.DeepEqual(f.Fingerprint, value.Fingerprint)
		})
	}
	var previous []models.Fingerprint
	for _, row := range rows {
		if types != nil && !slices.Contains(types, row.Type) {
			continue
		}
		old := models.Fingerprint{Type: row.Type, Fingerprint: row.Fingerprint}
		previous = append(previous, old)
		if !contains(next, old) {
			if _, err := dbWrapper.Exec(ctx, "DELETE FROM files_fingerprints WHERE file_id=? AND type=? AND fingerprint=?", fileID, old.Type, old.Fingerprint); err != nil {
				return err
			}
		}
	}
	for _, value := range next {
		if !contains(previous, value) {
			if err := qb.insert(ctx, fileID, value); err != nil {
				return err
			}
			previous = append(previous, value)
		}
	}
	return nil
}

func (qb *fingerprintQueryBuilder) destroyJoins(ctx context.Context, fileID models.FileID, types []string) error {
	table := qb.table()
	q := dialect.Delete(table).Where(
		table.Col(fileIDColumn).Eq(fileID),
		table.Col("type").In(types),
	)

	_, err := exec(ctx, q)
	if err != nil {
		return fmt.Errorf("deleting from %s: %w", table.GetTable(), err)
	}

	return nil
}

func (qb *fingerprintQueryBuilder) table() exp.IdentifierExpression {
	return qb.tableMgr.table
}
