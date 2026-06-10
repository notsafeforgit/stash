package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func forkColumnExists(ctx context.Context, db *sqlx.DB, tableName string, columnName string) (bool, error) {
	rows, err := db.QueryxContext(ctx, fmt.Sprintf("PRAGMA table_info(`%s`)", tableName))
	if err != nil {
		return false, fmt.Errorf("reading columns for %s: %w", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return false, fmt.Errorf("scanning columns for %s: %w", tableName, err)
		}
		if name == columnName {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("reading columns for %s: %w", tableName, err)
	}

	return false, nil
}
