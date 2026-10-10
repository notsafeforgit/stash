package sqlite

import (
	"errors"

	"github.com/jmoiron/sqlx"
)

func validateInterruptedWorkSchema(conn *sqlx.DB) error {
	var present bool
	if err := conn.Get(&present, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('archive_jobs') WHERE name='failures')"); err != nil {
		return err
	}
	if !present {
		return errors.New("native database schema is incomplete: missing archive job failure budget")
	}
	return nil
}
