package migrations

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/sqlite"
)

func fork004VideoStreamMetadata(ctx context.Context, db *sqlx.DB) error {
	columns := []struct {
		columnName string
		columnType string
	}{
		{columnName: "video_stream_duration", columnType: "real"},
		{columnName: "frame_count", columnType: "integer"},
		{columnName: "duration_mismatch", columnType: "boolean not null default false"},
	}

	for _, column := range columns {
		exists, err := forkColumnExists(ctx, db, "video_files", column.columnName)
		if err != nil {
			return err
		}
		if exists {
			continue
		}

		if _, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE `video_files` ADD COLUMN `%s` %s", column.columnName, column.columnType)); err != nil {
			return fmt.Errorf("adding video_files.%s: %w", column.columnName, err)
		}
	}

	return nil
}

func init() {
	sqlite.RegisterForkMigration(4, "video stream metadata", fork004VideoStreamMetadata)
}
