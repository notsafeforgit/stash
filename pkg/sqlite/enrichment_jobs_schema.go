package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func validateEnrichmentJobSchema(conn *sqlx.DB, releases, handoffs bool, auditData bool) error {
	for _, object := range []struct{ name, kind string }{
		{"enrichment_job_targets", "table"}, {"enrichment_job_attempts", "table"},
		{"enrichment_checkpoint_receipts", "table"}, {"enrichment_checkpoints", "table"},
		{"enrichment_checkpoint_records", "table"}, {"enrichment_checkpoint_usage", "table"},
		{"archive_jobs_enrichment_target", "index"}, {"enrichment_job_attempts_producer", "index"}, {"enrichment_checkpoint_records_receipt", "index"},
		{"enrichment_job_target_immutable", "trigger"}, {"enrichment_job_target_scope", "trigger"},
		{"enrichment_job_attempt_immutable", "trigger"}, {"enrichment_job_attempt_scope", "trigger"},
		{"enrichment_checkpoint_receipt_immutable", "trigger"}, {"enrichment_checkpoint_receipt_scope", "trigger"},
		{"enrichment_checkpoint_initial", "trigger"}, {"enrichment_checkpoint_transition", "trigger"},
		{"enrichment_checkpoint_record_immutable", "trigger"}, {"enrichment_checkpoint_record_scope", "trigger"},
		{"enrichment_checkpoint_usage_insert", "trigger"}, {"enrichment_checkpoint_usage_update", "trigger"}, {"enrichment_checkpoint_usage_delete", "trigger"},
	} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=? AND type=?)", object.name, object.kind); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", object.name)
		}
	}
	var definition string
	if err := conn.Get(&definition, "SELECT sql FROM sqlite_schema WHERE name='archive_jobs' AND type='table'"); err != nil {
		return err
	}
	if !strings.Contains(definition, "'post.enrich'") {
		return errors.New("native database schema is incomplete: missing enrichment archive job kind")
	}
	var invalid bool
	if err := conn.Get(&invalid, "SELECT NOT EXISTS(SELECT 1 FROM pragma_table_info('enrichment_checkpoint_receipts') WHERE name='created_at' AND upper(type)='DATETIME')"); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has an invalid enrichment checkpoint timestamp type")
	}
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM enrichment_job_targets b
 LEFT JOIN archive_jobs j ON j.uuid=b.job_uuid LEFT JOIN enrichment_targets t ON t.uuid=b.target_uuid
 LEFT JOIN enrichment_target_history h ON h.target_uuid=b.target_uuid AND h.revision=b.target_revision
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=t.collection_uuid AND c.revision=t.collection_revision
 WHERE j.uuid IS NULL OR j.kind!='post.enrich' OR h.state IS NOT 'pending'
 OR b.target_uuid IS NOT json_extract(j.arguments,'$.target_uuid') OR b.target_revision IS NOT json_extract(j.arguments,'$.target_revision')
 OR t.post_uuid IS NOT json_extract(j.arguments,'$.post_uuid') OR t.collection_uuid IS NOT json_extract(j.arguments,'$.collection_uuid')
 OR t.collection_revision IS NOT json_extract(j.arguments,'$.collection_revision') OR c.collection_uuid IS NULL
 OR c.root_uuid IS NOT json_extract(j.arguments,'$.root_uuid'))
 OR EXISTS(SELECT 1 FROM archive_jobs j WHERE j.kind='post.enrich'
  AND NOT EXISTS(SELECT 1 FROM enrichment_job_targets b WHERE b.job_uuid=j.uuid))
 OR EXISTS(SELECT 1 FROM enrichment_job_attempts p LEFT JOIN archive_job_attempts a ON a.job_uuid=p.job_uuid AND a.fence=p.fence
  LEFT JOIN enrichment_job_targets b ON b.job_uuid=p.job_uuid LEFT JOIN ingest_producers i ON i.uuid=p.producer_uuid
  WHERE a.job_uuid IS NULL OR b.job_uuid IS NULL OR i.uuid IS NULL)
 OR EXISTS(SELECT 1 FROM archive_job_attempts a JOIN archive_jobs j ON j.uuid=a.job_uuid AND j.kind='post.enrich'
  WHERE NOT EXISTS(SELECT 1 FROM enrichment_job_attempts p WHERE p.job_uuid=a.job_uuid AND p.fence=a.fence))`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	if !handoffs {
		if err := conn.Get(&invalid, "SELECT EXISTS(SELECT 1 FROM archive_jobs WHERE kind='post.enrich' AND json_extract(arguments,'$.version')!=1)"); err != nil {
			return err
		}
		if invalid {
			return models.ErrSourcePayloadCorrupt
		}
	}
	if err := validateEnrichmentJobArguments(conn); err != nil {
		return err
	}
	receiptHead := "enrichment_checkpoints"
	if releases {
		receiptHead = `(SELECT job_uuid,revision FROM enrichment_checkpoints UNION ALL
 SELECT p.job_uuid,p.checkpoint_revision revision FROM enrichment_publications p
 JOIN enrichment_checkpoint_releases x ON x.job_uuid=p.job_uuid)`
	}
	if err := conn.Get(&invalid, `SELECT
 (SELECT count(*) FROM enrichment_checkpoint_usage)!=1
 OR NOT EXISTS(SELECT 1 FROM enrichment_checkpoint_usage WHERE singleton=1 AND byte_size=coalesce((SELECT sum(byte_size) FROM enrichment_checkpoints),0)
  AND byte_size BETWEEN 0 AND 2147483648)
 OR EXISTS(SELECT 1 FROM enrichment_checkpoints h LEFT JOIN enrichment_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision
  WHERE r.job_uuid IS NULL OR h.revision!=(SELECT count(*) FROM enrichment_checkpoint_receipts p WHERE p.job_uuid=h.job_uuid)
  OR h.revision!=(SELECT max(revision) FROM enrichment_checkpoint_receipts p WHERE p.job_uuid=h.job_uuid)
  OR h.byte_size!=length(CAST(h.body AS BLOB)) OR h.byte_size NOT BETWEEN 1 AND 33554432
  OR r.record_count!=(SELECT count(*) FROM enrichment_checkpoint_records p WHERE p.job_uuid=h.job_uuid))
 OR EXISTS(SELECT 1 FROM enrichment_checkpoint_receipts r LEFT JOIN `+receiptHead+` h ON h.job_uuid=r.job_uuid
  LEFT JOIN enrichment_checkpoint_receipts previous ON previous.job_uuid=r.job_uuid AND previous.revision=r.revision-1
  LEFT JOIN enrichment_job_attempts a ON a.job_uuid=r.job_uuid AND a.fence=r.fence
  WHERE h.job_uuid IS NULL OR a.job_uuid IS NULL OR r.revision NOT BETWEEN 1 AND h.revision
  OR r.record_count NOT BETWEEN 1 AND 1024 OR r.pending_count NOT BETWEEN 0 AND 256 OR r.unresolved_count NOT BETWEEN 0 AND 256
  OR (r.revision>1 AND (previous.revision IS NULL OR r.record_count<previous.record_count
   OR r.unresolved_count<previous.unresolved_count OR r.fence<previous.fence OR r.created_at<previous.created_at))
  OR r.record_count-coalesce(previous.record_count,0)!=(SELECT count(*) FROM enrichment_checkpoint_records p
    WHERE p.job_uuid=r.job_uuid AND p.checkpoint_revision=r.revision))
 OR EXISTS(SELECT 1 FROM enrichment_checkpoint_records p LEFT JOIN enrichment_checkpoint_receipts r
  ON r.job_uuid=p.job_uuid AND r.revision=p.checkpoint_revision
  WHERE r.job_uuid IS NULL OR p.ordinal>=r.record_count OR p.ordinal<coalesce((SELECT previous.record_count
   FROM enrichment_checkpoint_receipts previous WHERE previous.job_uuid=r.job_uuid AND previous.revision=r.revision-1),0))`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	if err := validateEnrichmentCheckpointReceipts(conn); err != nil {
		return err
	}
	return validateEnrichmentCheckpoints(conn)
}

func validateEnrichmentJobArguments(conn *sqlx.DB) error {
	rows, err := conn.Queryx("SELECT * FROM archive_jobs WHERE kind='post.enrich'")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row archiveJobRow
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if _, err := archive.DecodeEnrichmentJob(row.resolve()); err != nil {
			return err
		}
	}
	return rows.Err()
}

func validateEnrichmentCheckpointReceipts(conn *sqlx.DB) error {
	rows, err := conn.Queryx(`SELECT r.*,a.started_at_ms,a.ended_at_ms FROM enrichment_checkpoint_receipts r
 JOIN archive_job_attempts a ON a.job_uuid=r.job_uuid AND a.fence=r.fence`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row struct {
			models.EnrichmentCheckpointReceipt
			Started int64  `db:"started_at_ms"`
			Ended   *int64 `db:"ended_at_ms"`
		}
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if !validJobTime(row.CreatedAt) || row.CreatedAt.UnixMilli() < row.Started ||
			(row.Ended != nil && row.CreatedAt.UnixMilli() > *row.Ended) || !archive.ValidSHA256(row.Digest) ||
			row.Revision < 1 || row.Revision > archive.MaxEnrichmentCheckpointRevisions {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return rows.Err()
}

func validateEnrichmentCheckpoints(conn *sqlx.DB) error {
	// Read one bounded body at a time. The single read connection must be free
	// for its indexed provenance lookup, without duplicating bodies in a join.
	after := ""
	for {
		var row struct {
			models.EnrichmentCheckpointReceipt
			Body             string `db:"body"`
			URL              string `db:"url"`
			ExtractorVersion string `db:"extractor_version"`
		}
		err := conn.Get(&row, `SELECT r.*,h.body,u.url,json_extract(j.arguments,'$.extractor_version') extractor_version
 FROM enrichment_checkpoints h JOIN enrichment_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision
 JOIN enrichment_job_targets b ON b.job_uuid=h.job_uuid JOIN archive_jobs j ON j.uuid=h.job_uuid
 JOIN enrichment_targets t ON t.uuid=b.target_uuid JOIN source_post_urls u ON u.uuid=t.url_uuid
 WHERE h.job_uuid>? ORDER BY h.job_uuid LIMIT 1`, after)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		parsed, err := archive.ParseEnrichmentTranscript([]byte(row.Body))
		if err != nil || enrichmentDigest([]byte(row.Body)) != row.Digest || parsed.URL != row.URL || parsed.ExtractorVersion != row.ExtractorVersion ||
			len(parsed.Records) != row.RecordCount || len(parsed.Pending) != row.PendingCount || len(parsed.Unresolved) != row.UnresolvedCount {
			return models.ErrSourcePayloadCorrupt
		}
		var jobRow archiveJobRow
		if err := conn.Get(&jobRow, "SELECT * FROM archive_jobs WHERE uuid=?", row.JobUUID); err != nil {
			return err
		}
		job := jobRow.resolve()
		work, err := archive.DecodeEnrichmentJob(job)
		if err != nil {
			return err
		}
		if err := verifyEnrichmentTranscript(conn.Get, conn.Select, job, work, parsed); err != nil {
			return err
		}
		var records []struct {
			Ordinal int    `db:"ordinal"`
			Digest  string `db:"digest"`
		}
		if err := conn.Select(&records, "SELECT ordinal,digest FROM enrichment_checkpoint_records WHERE job_uuid=? ORDER BY ordinal LIMIT 1025", row.JobUUID); err != nil {
			return err
		}
		if len(records) != row.RecordCount {
			return models.ErrSourcePayloadCorrupt
		}
		for i, record := range records {
			digest, err := parsed.RecordDigest(i)
			if err != nil || record.Ordinal != i || record.Digest != digest {
				return models.ErrSourcePayloadCorrupt
			}
		}
		after = row.JobUUID
	}
}
