package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateAttachmentDownloadSchema(conn *sqlx.DB) error {
	for _, name := range []string{"source_attachment_downloads", "attachment_download_transfer_phase", "attachment_download_sequence_phase", "attachment_download_history", "attachment_download_immutable", "attachment_download_scope", "attachment_download_receipt"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_attachment_downloads d
 LEFT JOIN ingest_receipts r ON r.producer_uuid=d.producer_uuid AND r.event_uuid=d.event_uuid
 LEFT JOIN ingest_receipts c ON c.producer_uuid=d.producer_uuid AND c.event_uuid=d.capture_event_uuid
 LEFT JOIN source_runs s ON s.uuid=d.run_uuid
 LEFT JOIN source_run_attempts a ON a.run_uuid=d.run_uuid AND a.fence=d.fence
 WHERE r.kind IS NOT 'attachment.download' OR c.kind IS NOT 'source.capture' OR s.operation IS NOT 'download'
 OR a.producer_uuid IS NOT d.producer_uuid OR a.owner_uuid IS NOT d.owner_uuid
 OR r.run_uuid IS NOT d.run_uuid OR c.run_uuid IS NOT d.run_uuid
 OR r.collection_uuid IS NOT c.collection_uuid OR r.collection_revision IS NOT c.collection_revision OR r.root_uuid IS NOT c.root_uuid
 OR c.collection_uuid IS NOT s.collection_uuid OR c.collection_revision IS NOT s.collection_revision OR c.root_uuid IS NOT s.root_uuid
 OR r.capture_uuid IS NOT c.capture_uuid OR r.post_uuid IS NOT c.post_uuid
 OR NOT EXISTS(SELECT 1 FROM source_capture_attachment_manifests m JOIN source_attachment_entries e ON e.manifest_uuid=m.manifest_uuid
 WHERE m.capture_uuid=c.capture_uuid AND e.attachment_uuid=d.attachment_uuid))
 OR EXISTS(SELECT 1 FROM ingest_receipts r WHERE r.kind='attachment.download' AND NOT EXISTS(
 SELECT 1 FROM source_attachment_downloads d WHERE d.producer_uuid=r.producer_uuid AND d.event_uuid=r.event_uuid))
 OR EXISTS(SELECT 1 FROM source_attachment_downloads d JOIN source_attachment_downloads o
 ON o.producer_uuid=d.producer_uuid AND o.capture_event_uuid=d.capture_event_uuid AND o.attachment_uuid=d.attachment_uuid AND o.phase!=d.phase
 WHERE o.run_uuid!=d.run_uuid OR o.fence!=d.fence OR o.owner_uuid!=d.owner_uuid OR o.transfer_sequence!=d.transfer_sequence)
 OR EXISTS(SELECT 1 FROM source_attachment_downloads d JOIN source_attachment_downloads o
 ON o.producer_uuid=d.producer_uuid AND o.run_uuid=d.run_uuid AND o.fence=d.fence AND o.transfer_sequence=d.transfer_sequence AND o.phase!=d.phase
 WHERE o.capture_event_uuid!=d.capture_event_uuid OR o.attachment_uuid!=d.attachment_uuid)
 OR EXISTS(SELECT 1 FROM source_attachment_downloads d JOIN ingest_receipts c ON c.producer_uuid=d.producer_uuid AND c.event_uuid=d.capture_event_uuid
 LEFT JOIN ingest_receipts f ON f.producer_uuid=d.producer_uuid AND f.event_uuid=d.file_event_uuid
 LEFT JOIN archive_jobs j ON j.uuid=f.job_uuid WHERE d.file_event_uuid IS NOT NULL AND
 (f.kind IS NOT 'file.completed' OR f.run_uuid IS NOT d.run_uuid OR f.collection_uuid IS NOT c.collection_uuid
 OR f.collection_revision IS NOT c.collection_revision OR f.root_uuid IS NOT c.root_uuid OR f.capture_uuid IS NOT c.capture_uuid
 OR j.kind IS NOT 'media.verify' OR json_extract(j.arguments,'$.publication.source.capture_uuid') IS NOT c.capture_uuid
 OR json_extract(j.arguments,'$.publication.source.attachment_uuid') IS NOT d.attachment_uuid))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has inconsistent attachment download reports")
	}
	return nil
}
