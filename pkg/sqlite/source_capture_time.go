package sqlite

import (
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

func canonicalCaptureTime(input *models.SourceCaptureInput) error {
	valid := func(value time.Time) bool {
		return !value.IsZero() && value.UTC().Year() >= 1 && value.UTC().Year() <= 9999
	}
	if input.CapturedAt.IsZero() {
		if input.RecordedAt == nil || !valid(*input.RecordedAt) {
			return errors.New("unrecorded observation requires an archive recording time")
		}
		stamp := input.RecordedAt.UTC()
		input.RecordedAt = &stamp
		return nil
	}
	if !valid(input.CapturedAt) || input.RecordedAt != nil {
		return errors.New("capture must distinguish observation time from an unrecorded observation")
	}
	input.CapturedAt = input.CapturedAt.UTC()
	return nil
}

func validateSourceCaptureTimeSchema(conn *sqlx.DB, auditData bool) error {
	var valid bool
	if err := conn.Get(&valid, `SELECT EXISTS(SELECT 1 FROM pragma_table_info('source_captures') WHERE name='captured_at' AND "notnull"=0 AND upper(type)='DATETIME')
 AND EXISTS(SELECT 1 FROM pragma_table_info('source_captures') WHERE name='recorded_at' AND "notnull"=0 AND upper(type)='DATETIME')
 AND EXISTS(SELECT 1 FROM sqlite_schema WHERE name='source_captures_order' AND type='index')
 AND EXISTS(SELECT 1 FROM sqlite_schema WHERE name='source_captures_unrecorded' AND type='index')`); err != nil {
		return err
	}
	if !valid {
		return errors.New("native database is missing nullable observation and archive recording times")
	}
	if !auditData {
		return nil
	}
	if err := conn.Get(&valid, `SELECT NOT EXISTS(SELECT 1 FROM source_captures
 WHERE (captured_at IS NULL)=(recorded_at IS NULL))`); err != nil {
		return err
	}
	if !valid {
		return models.ErrSourcePayloadCorrupt
	}
	// Known observations keep their original signatures. Validate all new
	// unknown-time records, including their archive timestamp and exact payload.
	for after := ""; ; {
		var id string
		err := conn.Get(&id, "SELECT coalesce(min(uuid),'') FROM source_captures WHERE captured_at IS NULL AND uuid>?", after)
		if err != nil || id == "" {
			return err
		}
		if _, err := findSourceCapture(conn.Get, conn.Select, id); err != nil {
			return err
		}
		after = id
	}
}
