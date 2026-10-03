package sqlite

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func validateEnrichmentPublicationSchema(conn *sqlx.DB) error {
	for _, object := range []struct{ name, kind string }{
		{"enrichment_publications", "table"}, {"enrichment_published_records", "table"},
		{"enrichment_published_records_capture", "index"}, {"enrichment_publication_immutable", "trigger"},
		{"enrichment_publication_scope", "trigger"}, {"enrichment_published_record_immutable", "trigger"},
		{"enrichment_published_record_scope", "trigger"}, {"enrichment_job_success", "trigger"},
	} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=? AND type=?)", object.name, object.kind); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", object.name)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT
 EXISTS(SELECT 1 FROM archive_jobs j WHERE j.kind='post.enrich' AND j.state='succeeded'
  AND NOT EXISTS(SELECT 1 FROM enrichment_publications p WHERE p.job_uuid=j.uuid))
 OR EXISTS(SELECT 1 FROM enrichment_publications p
  LEFT JOIN archive_jobs j ON j.uuid=p.job_uuid LEFT JOIN enrichment_job_targets b ON b.job_uuid=p.job_uuid
  LEFT JOIN enrichment_job_attempts a ON a.job_uuid=p.job_uuid AND a.fence=p.fence
  LEFT JOIN enrichment_checkpoints h ON h.job_uuid=p.job_uuid AND h.revision=p.checkpoint_revision
  LEFT JOIN enrichment_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision
  LEFT JOIN enrichment_completions e ON e.uuid=p.completion_uuid
  LEFT JOIN enrichment_targets t ON t.uuid=b.target_uuid
  WHERE j.uuid IS NULL OR j.kind!='post.enrich' OR j.state!='succeeded' OR j.fence!=p.fence OR a.job_uuid IS NULL
  OR h.job_uuid IS NULL OR r.pending_count!=0 OR r.fence>p.fence OR r.created_at>p.created_at
  OR e.uuid IS NULL OR e.target_uuid IS NOT b.target_uuid OR e.expected_revision IS NOT b.target_revision OR e.created_at!=p.created_at
  OR t.state IS NOT 'completed' OR t.completion_uuid IS NOT e.uuid OR t.revision!=b.target_revision+1
  OR r.record_count!=(SELECT count(*) FROM enrichment_published_records c WHERE c.job_uuid=p.job_uuid)
  OR e.capture_count!=(SELECT count(DISTINCT capture_uuid) FROM enrichment_published_records c WHERE c.job_uuid=p.job_uuid))
 OR EXISTS(SELECT 1 FROM enrichment_published_records p
  LEFT JOIN enrichment_publications b ON b.job_uuid=p.job_uuid
  LEFT JOIN enrichment_checkpoint_records r ON r.job_uuid=p.job_uuid AND r.ordinal=p.ordinal
  LEFT JOIN enrichment_completion_captures c ON c.completion_uuid=b.completion_uuid AND c.capture_uuid=p.capture_uuid
  WHERE b.job_uuid IS NULL OR r.job_uuid IS NULL OR c.capture_uuid IS NULL)`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	for after := ""; ; {
		var publication models.EnrichmentPublication
		err := conn.Get(&publication, enrichmentPublicationSelect+" WHERE p.job_uuid>? ORDER BY p.job_uuid LIMIT 1", after)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var row archiveJobRow
		if err := conn.Get(&row, "SELECT * FROM archive_jobs WHERE uuid=?", publication.JobUUID); err != nil {
			return err
		}
		job := row.resolve()
		work, err := archive.DecodeEnrichmentJob(job)
		if err != nil {
			return err
		}
		completion, err := archive.EnrichmentCompletionUUID(job.UUID)
		if err != nil {
			return err
		}
		result, err := archive.EnrichmentPublicationResult(publication)
		if err != nil || completion != publication.CompletionUUID || !bytes.Equal(result, job.Result) || !validJobTime(publication.CreatedAt) || publication.CreatedAt.UnixMilli() != job.UpdatedAt.UnixMilli() {
			return models.ErrSourcePayloadCorrupt
		}
		var body string
		if err := conn.Get(&body, "SELECT body FROM enrichment_checkpoints WHERE job_uuid=?", job.UUID); err != nil {
			return err
		}
		transcript, err := archive.ParseEnrichmentTranscript([]byte(body))
		if err != nil {
			return err
		}
		var records []models.EnrichmentPublishedRecord
		if err := conn.Select(&records, enrichmentPublishedRecordsSelect+" WHERE p.job_uuid=? ORDER BY p.ordinal LIMIT 1025", job.UUID); err != nil {
			return err
		}
		if len(records) != len(transcript.Records) {
			return models.ErrSourcePayloadCorrupt
		}
		for i, record := range records {
			input, post, err := archive.PrepareEnrichmentCapture(job.UUID, work, transcript, record.EnrichmentCheckpointRecord)
			if err != nil {
				return err
			}
			if record.Ordinal != i || record.CaptureUUID != input.UUID {
				return models.ErrSourcePayloadCorrupt
			}
			if err := verifyEnrichmentCapture(conn.Get, conn.Select, *input, *post, work.CollectionUUID, work.CollectionRevision); err != nil {
				return err
			}
		}
		after = job.UUID
	}
}
