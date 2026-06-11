package migrations

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/sqlite"
)

func fork003FileColorMetadata(ctx context.Context, db *sqlx.DB) error {
	columns := []struct {
		tableName  string
		columnName string
		columnType string
	}{
		{tableName: "video_files", columnName: "bit_depth", columnType: "integer"},
		{tableName: "video_files", columnName: "color_range", columnType: "varchar(255)"},
		{tableName: "video_files", columnName: "color_space", columnType: "varchar(255)"},
		{tableName: "video_files", columnName: "color_transfer", columnType: "varchar(255)"},
		{tableName: "video_files", columnName: "color_primaries", columnType: "varchar(255)"},
		{tableName: "image_files", columnName: "bit_depth", columnType: "integer"},
		{tableName: "image_files", columnName: "color_range", columnType: "varchar(255)"},
		{tableName: "image_files", columnName: "color_space", columnType: "varchar(255)"},
		{tableName: "image_files", columnName: "color_transfer", columnType: "varchar(255)"},
		{tableName: "image_files", columnName: "color_primaries", columnType: "varchar(255)"},
	}

	for _, column := range columns {
		exists, err := forkColumnExists(ctx, db, column.tableName, column.columnName)
		if err != nil {
			return err
		}
		if exists {
			continue
		}

		if _, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `%s` %s", column.tableName, column.columnName, column.columnType)); err != nil {
			return fmt.Errorf("adding %s.%s: %w", column.tableName, column.columnName, err)
		}
	}

	return nil
}

func init() {
	sqlite.RegisterForkMigration(3, "file color metadata", fork003FileColorMetadata)
}
