package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func validateDiscoveryDetailSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"discovery_detail_jobs", "discovery_detail_job_immutable", "discovery_detail_job_scope", "archive_jobs_discovery_detail", "discovery_detail_pacing_bind", "discovery_detail_attempts", "discovery_detail_attempt_immutable", "discovery_detail_attempt_scope", "discovery_detail_checkpoint_receipts", "discovery_detail_checkpoint_receipt_immutable", "discovery_detail_checkpoint_receipt_scope", "discovery_detail_checkpoints", "discovery_detail_checkpoint_initial", "discovery_detail_checkpoint_transition", "discovery_detail_checkpoint_records", "discovery_detail_checkpoint_records_receipt", "discovery_detail_checkpoint_record_immutable", "discovery_detail_checkpoint_record_scope", "discovery_detail_checkpoint_usage", "discovery_detail_checkpoint_usage_insert", "discovery_detail_checkpoint_usage_update", "discovery_detail_checkpoint_usage_delete", "discovery_detail_results", "discovery_detail_result_immutable", "discovery_detail_result_scope", "discovery_detail_success"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, `SELECT
 EXISTS(SELECT 1 FROM archive_jobs j WHERE j.kind='post.verify_candidate' AND NOT EXISTS(SELECT 1 FROM discovery_detail_jobs b WHERE b.job_uuid=j.uuid))
 OR EXISTS(SELECT 1 FROM discovery_detail_jobs b JOIN archive_jobs j ON j.uuid=b.job_uuid WHERE j.kind!='post.verify_candidate')
 OR EXISTS(SELECT 1 FROM archive_job_attempts a JOIN archive_jobs j ON j.uuid=a.job_uuid AND j.kind='post.verify_candidate'
 WHERE NOT EXISTS(SELECT 1 FROM discovery_detail_attempts d WHERE d.job_uuid=a.job_uuid AND d.fence=a.fence))
 OR EXISTS(SELECT 1 FROM discovery_detail_attempts d LEFT JOIN archive_job_attempts a ON a.job_uuid=d.job_uuid AND a.fence=d.fence
 LEFT JOIN discovery_detail_jobs b ON b.job_uuid=d.job_uuid LEFT JOIN ingest_producers p ON p.uuid=d.producer_uuid
 WHERE a.job_uuid IS NULL OR b.job_uuid IS NULL OR p.uuid IS NULL)
 OR (SELECT count(*) FROM archive_jobs WHERE kind='post.verify_candidate' AND state IN ('queued','running'))>?
 OR (SELECT count(*) FROM discovery_detail_checkpoint_usage)!=1
 OR NOT EXISTS(SELECT 1 FROM discovery_detail_checkpoint_usage WHERE singleton=1 AND byte_size=coalesce((SELECT sum(byte_size) FROM discovery_detail_checkpoints),0))
 OR EXISTS(SELECT 1 FROM discovery_detail_checkpoints h LEFT JOIN discovery_detail_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision
 WHERE r.job_uuid IS NULL OR h.revision!=(SELECT count(*) FROM discovery_detail_checkpoint_receipts p WHERE p.job_uuid=h.job_uuid)
 OR h.revision!=(SELECT max(revision) FROM discovery_detail_checkpoint_receipts p WHERE p.job_uuid=h.job_uuid)
 OR h.byte_size!=length(CAST(h.body AS BLOB)) OR h.byte_size NOT BETWEEN 1 AND 33554432
 OR r.record_count!=(SELECT count(*) FROM discovery_detail_checkpoint_records p WHERE p.job_uuid=h.job_uuid))
 OR EXISTS(SELECT 1 FROM discovery_detail_checkpoint_receipts r LEFT JOIN discovery_detail_checkpoints h ON h.job_uuid=r.job_uuid
 LEFT JOIN discovery_detail_checkpoint_receipts previous ON previous.job_uuid=r.job_uuid AND previous.revision=r.revision-1
 LEFT JOIN discovery_detail_attempts a ON a.job_uuid=r.job_uuid AND a.fence=r.fence
 WHERE h.job_uuid IS NULL OR a.job_uuid IS NULL OR r.revision NOT BETWEEN 1 AND h.revision
 OR r.record_count NOT BETWEEN 0 AND 1024 OR r.pending_count NOT BETWEEN 0 AND 256 OR r.unresolved_count NOT BETWEEN 0 AND 256
 OR (r.revision>1 AND (previous.revision IS NULL OR r.record_count<previous.record_count OR r.unresolved_count<previous.unresolved_count OR r.fence<previous.fence OR r.created_at<previous.created_at))
 OR r.record_count-coalesce(previous.record_count,0)!=(SELECT count(*) FROM discovery_detail_checkpoint_records p WHERE p.job_uuid=r.job_uuid AND p.checkpoint_revision=r.revision))
 OR EXISTS(SELECT 1 FROM discovery_detail_checkpoint_records p LEFT JOIN discovery_detail_checkpoint_receipts r ON r.job_uuid=p.job_uuid AND r.revision=p.checkpoint_revision
 WHERE r.job_uuid IS NULL OR p.ordinal>=r.record_count OR p.ordinal<coalesce((SELECT previous.record_count FROM discovery_detail_checkpoint_receipts previous WHERE previous.job_uuid=r.job_uuid AND previous.revision=r.revision-1),0))
 OR EXISTS(SELECT 1 FROM discovery_detail_results r JOIN archive_jobs j ON j.uuid=r.job_uuid WHERE j.kind!='post.verify_candidate' OR j.state!='succeeded' OR j.fence!=r.fence)
 OR EXISTS(SELECT 1 FROM archive_jobs j WHERE j.kind='post.verify_candidate' AND j.state='succeeded' AND NOT EXISTS(SELECT 1 FROM discovery_detail_results r WHERE r.job_uuid=j.uuid))`, archive.MaxDiscoveryDetailJobs); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	if err := validateDiscoveryDetailReceipts(conn); err != nil {
		return err
	}
	for after := ""; ; {
		var row archiveJobRow
		err := conn.Get(&row, "SELECT * FROM archive_jobs WHERE kind='post.verify_candidate' AND uuid>? ORDER BY uuid LIMIT 1", after)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		job := row.resolve()
		work, err := discoveryDetailJobProof(conn.Get, job)
		if err != nil {
			return err
		}
		var bound bool
		scope, err := scrape.SourceScopeV1(work.URL)
		if err != nil {
			return err
		}
		if err := conn.Get(&bound, "SELECT EXISTS(SELECT 1 FROM enrichment_job_pacing WHERE job_uuid=? AND scope=?)", job.UUID, scope); err != nil {
			return err
		}
		if !bound {
			return models.ErrSourcePayloadCorrupt
		}
		if work.Generation > 1 {
			var previous archiveJobRow
			err := conn.Get(&previous, `SELECT j.* FROM discovery_detail_jobs b JOIN archive_jobs j ON j.uuid=b.job_uuid
 WHERE b.target_uuid=? AND b.target_revision=? AND b.candidate_sequence=? AND b.generation=?`, work.TargetUUID, work.TargetRevision, work.CandidateSequence, work.Generation-1)
			if err != nil {
				return models.ErrSourcePayloadCorrupt
			}
			prior := previous.resolve()
			old, err := archive.DecodeDiscoveryDetailJob(prior)
			if err != nil {
				return err
			}
			old.Generation++
			if !reflect.DeepEqual(old, work) || (prior.State != "failed" && prior.State != "cancelled") || job.CreatedAt.Before(prior.UpdatedAt) || job.AvailableAt.Before(prior.AvailableAt) {
				return models.ErrSourcePayloadCorrupt
			}
		}
		if err := validateDiscoveryDetailBody(conn, job, work); err != nil {
			return err
		}
		after = job.UUID
	}
}

func validateDiscoveryDetailReceipts(conn *sqlx.DB) error {
	rows, err := conn.Queryx(`SELECT r.*,a.started_at_ms,a.ended_at_ms FROM discovery_detail_checkpoint_receipts r
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
		if !validJobTime(row.CreatedAt) || row.CreatedAt.UnixMilli() < row.Started || (row.Ended != nil && row.CreatedAt.UnixMilli() > *row.Ended) || !archive.ValidSHA256(row.Digest) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return rows.Err()
}

func validateDiscoveryDetailBody(conn *sqlx.DB, job *models.ArchiveJob, work *models.DiscoveryDetailJobArguments) error {
	var head struct {
		models.EnrichmentCheckpointReceipt
		Body string `db:"body"`
	}
	err := conn.Get(&head, `SELECT r.*,h.body FROM discovery_detail_checkpoints h JOIN discovery_detail_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision WHERE h.job_uuid=?`, job.UUID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	parsed, err := archive.ParseDiscoveryDetail([]byte(head.Body), work.PostIdentifier(), work.URL, work.ExtractorVersion)
	if err != nil || enrichmentDigest([]byte(head.Body)) != head.Digest || len(parsed.Records) != head.RecordCount || len(parsed.Pending) != head.PendingCount || len(parsed.Unresolved) != head.UnresolvedCount {
		return models.ErrSourcePayloadCorrupt
	}
	var records []struct {
		Ordinal   int       `db:"ordinal"`
		Digest    string    `db:"digest"`
		CreatedAt time.Time `db:"created_at"`
	}
	if err := conn.Select(&records, `SELECT p.ordinal,p.digest,r.created_at FROM discovery_detail_checkpoint_records p
 JOIN discovery_detail_checkpoint_receipts r ON r.job_uuid=p.job_uuid AND r.revision=p.checkpoint_revision WHERE p.job_uuid=? ORDER BY p.ordinal LIMIT 1025`, job.UUID); err != nil {
		return err
	}
	if len(records) != len(parsed.Records) {
		return models.ErrSourcePayloadCorrupt
	}
	for i, record := range records {
		digest, err := parsed.RecordDigest(i)
		if err != nil || record.Ordinal != i || record.Digest != digest {
			return models.ErrSourcePayloadCorrupt
		}
		observed, err := time.Parse(time.RFC3339Nano, parsed.Records[i].ObservedAt)
		if err != nil || observed.After(record.CreatedAt) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	if job.State != "succeeded" {
		return nil
	}
	var row discoveryDetailResultRow
	if err := conn.Get(&row, "SELECT * FROM discovery_detail_results WHERE job_uuid=?", job.UUID); err != nil {
		return err
	}
	result, err := row.resolve()
	if err != nil {
		return err
	}
	expected, err := compareDiscoveryDetail(conn.Get, work, parsed.Body())
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(*expected, result.Evidence) || result.Evidence.Status == "pending" || result.CheckpointRevision != head.Revision || result.CreatedAt.Before(head.CreatedAt) || result.CreatedAt.UnixMilli() != job.UpdatedAt.UnixMilli() {
		return models.ErrSourcePayloadCorrupt
	}
	var valid bool
	err = conn.Get(&valid, `SELECT EXISTS(SELECT 1 FROM archive_job_attempts a JOIN archive_jobs j ON j.uuid=a.job_uuid
 WHERE a.job_uuid=? AND a.fence=? AND a.outcome='succeeded' AND a.ended_at_ms=? AND a.result=j.result
 AND json_extract(j.result,'$.checkpoint_revision')=? AND json_extract(j.result,'$.checkpoint_sha256')=?)`, job.UUID, result.Fence, result.CreatedAt.UnixMilli(), head.Revision, head.Digest)
	if err != nil {
		return err
	}
	if !valid {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}
