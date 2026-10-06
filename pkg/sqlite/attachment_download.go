package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

func validAttachmentDownload(input models.AttachmentDownloadInput) bool {
	for _, id := range []string{input.ProducerUUID, input.EventUUID, input.RunUUID, input.OwnerUUID, input.CaptureEventUUID, input.AttachmentUUID} {
		if _, err := archiveUUID(id); err != nil {
			return false
		}
	}
	if input.FileEventUUID != "" {
		if _, err := archiveUUID(input.FileEventUUID); err != nil {
			return false
		}
	}
	const maxExact = 1<<53 - 1
	if input.Fence < 1 || input.Fence > maxExact || input.TransferSequence < 1 || input.TransferSequence > maxExact || input.ObservedAt.IsZero() || input.ObservedAt.UTC().Year() < 1 || input.ObservedAt.UTC().Year() > 9999 {
		return false
	}
	switch input.State {
	case "started":
		return input.FileEventUUID == "" && input.ReasonCode == ""
	case "downloaded":
		return input.FileEventUUID != "" && input.ReasonCode == ""
	case "failed":
		return input.FileEventUUID == "" && (input.ReasonCode == "download_failed" || input.ReasonCode == "postprocess_failed" || input.ReasonCode == "source_failure")
	case "excluded":
		return input.FileEventUUID == "" && (input.ReasonCode == "unsupported_media" || input.ReasonCode == "filter")
	case "skipped":
		return input.FileEventUUID == "" && (input.ReasonCode == "archive_entry_without_file" || input.ReasonCode == "existing_without_file")
	}
	return false
}

// RecordDownload must be followed by its ingestion receipt in the same managed
// transaction. A deferred foreign key prevents acknowledging only half of it.
func (s *SourceAttachmentStore) RecordDownload(ctx context.Context, input models.AttachmentDownloadInput) error {
	if !txn.HasHooks(ctx) || !validAttachmentDownload(input) {
		return models.ErrAttachmentDownloadInvalid
	}
	phase := 1
	if input.State == "started" {
		phase = 0
	}
	var conflict bool
	if err := dbWrapper.Get(ctx, &conflict, `SELECT EXISTS(SELECT 1 FROM source_attachment_downloads
 WHERE producer_uuid=? AND capture_event_uuid=? AND attachment_uuid=? AND phase=?)
 OR EXISTS(SELECT 1 FROM source_attachment_downloads WHERE producer_uuid=? AND run_uuid=? AND fence=? AND transfer_sequence=?
 AND (capture_event_uuid!=? OR attachment_uuid!=?))
 OR EXISTS(SELECT 1 FROM source_attachment_downloads WHERE producer_uuid=? AND capture_event_uuid=? AND attachment_uuid=?
 AND (run_uuid!=? OR fence!=? OR transfer_sequence!=?))`, input.ProducerUUID, input.CaptureEventUUID, input.AttachmentUUID, phase,
		input.ProducerUUID, input.RunUUID, input.Fence, input.TransferSequence, input.CaptureEventUUID, input.AttachmentUUID,
		input.ProducerUUID, input.CaptureEventUUID, input.AttachmentUUID, input.RunUUID, input.Fence, input.TransferSequence); err != nil {
		return err
	}
	if conflict {
		return models.ErrAttachmentDownloadConflict
	}
	file := sql.NullString{String: input.FileEventUUID, Valid: input.FileEventUUID != ""}
	ret, err := dbWrapper.Exec(ctx, `INSERT INTO source_attachment_downloads
 (producer_uuid,event_uuid,run_uuid,fence,owner_uuid,transfer_sequence,capture_event_uuid,attachment_uuid,state,phase,file_event_uuid,reason_code,observed_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, input.ProducerUUID, input.EventUUID, input.RunUUID, input.Fence, input.OwnerUUID, input.TransferSequence,
		input.CaptureEventUUID, input.AttachmentUUID, input.State, phase, file, input.ReasonCode, input.ObservedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	id, err := ret.LastInsertId()
	if err != nil {
		return err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		var valid bool
		if err := dbWrapper.Get(ctx, &valid, `SELECT EXISTS(SELECT 1 FROM source_attachment_downloads d JOIN ingest_receipts r
 ON r.producer_uuid=d.producer_uuid AND r.event_uuid=d.event_uuid AND r.kind='attachment.download' WHERE d.id=?)`, id); err != nil {
			return err
		}
		if !valid {
			return models.ErrAttachmentDownloadInvalid
		}
		return nil
	})
	return nil
}

// DownloadHistory pages original reports in server receipt order. TransferState
// describes that exact transfer, never an attachment-wide availability claim.
// A late started report cannot supersede an already retained terminal report.
func (s *SourceAttachmentStore) DownloadHistory(ctx context.Context, id string, after int64, limit int, now time.Time) ([]models.AttachmentDownloadReport, error) {
	if _, err := archiveUUID(id); err != nil || after < 0 || limit < 1 || limit > 100 || now.IsZero() {
		return nil, models.ErrAttachmentDownloadInvalid
	}
	var rows []struct {
		ID         int64          `db:"id"`
		Producer   string         `db:"producer_uuid"`
		Event      string         `db:"event_uuid"`
		Run        string         `db:"run_uuid"`
		Fence      int64          `db:"fence"`
		Owner      string         `db:"owner_uuid"`
		Transfer   int64          `db:"transfer_sequence"`
		Capture    string         `db:"capture_event_uuid"`
		Attachment string         `db:"attachment_uuid"`
		State      string         `db:"state"`
		File       sql.NullString `db:"file_event_uuid"`
		Reason     string         `db:"reason_code"`
		Observed   Timestamp      `db:"observed_at"`
		Recorded   Timestamp      `db:"recorded_at"`
		Collection string         `db:"collection_uuid"`
		Root       string         `db:"root_uuid"`
		Current    string         `db:"current_state"`
		JobState   string         `db:"job_state"`
		Job        string         `db:"job_uuid"`
	}
	err := dbWrapper.Select(ctx, &rows, `SELECT d.id,d.producer_uuid,d.event_uuid,d.run_uuid,d.fence,d.owner_uuid,d.transfer_sequence,
 d.capture_event_uuid,d.attachment_uuid,d.state,d.file_event_uuid,d.reason_code,d.observed_at,d.recorded_at,r.collection_uuid,r.root_uuid,
 CASE WHEN t.id IS NOT NULL THEN t.state WHEN r.state='running' AND r.fence=d.fence AND r.producer_uuid=d.producer_uuid
 AND r.owner_uuid=d.owner_uuid AND r.lease_until_ms>? THEN 'downloading' ELSE 'interrupted' END AS current_state,
 coalesce(j.state,'') AS job_state,coalesce(j.uuid,'') AS job_uuid
 FROM source_attachment_downloads d JOIN source_runs r ON r.uuid=d.run_uuid
 LEFT JOIN source_attachment_downloads t ON t.producer_uuid=d.producer_uuid AND t.capture_event_uuid=d.capture_event_uuid
 AND t.attachment_uuid=d.attachment_uuid AND t.phase=1
 LEFT JOIN ingest_receipts f ON f.producer_uuid=t.producer_uuid AND f.event_uuid=t.file_event_uuid
 LEFT JOIN archive_jobs j ON j.uuid=f.job_uuid
 WHERE d.attachment_uuid=? AND d.id>? ORDER BY d.id LIMIT ?`, now.UnixMilli(), id, after, limit)
	if err != nil {
		return nil, err
	}
	ret := make([]models.AttachmentDownloadReport, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, models.AttachmentDownloadReport{AttachmentDownloadInput: models.AttachmentDownloadInput{
			ProducerUUID: row.Producer, EventUUID: row.Event, RunUUID: row.Run, Fence: row.Fence, OwnerUUID: row.Owner,
			TransferSequence: row.Transfer, CaptureEventUUID: row.Capture, AttachmentUUID: row.Attachment, State: row.State,
			FileEventUUID: row.File.String, ReasonCode: row.Reason, ObservedAt: row.Observed.Timestamp}, Sequence: row.ID,
			RecordedAt: row.Recorded.Timestamp, CollectionUUID: row.Collection, RootUUID: row.Root, TransferState: row.Current,
			VerificationState: row.JobState, VerificationJob: row.Job})
	}
	return ret, nil
}
