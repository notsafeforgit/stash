package sqlite

import (
	"context"
	"math"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// Only one of the at-most-two reports is the transfer's pagination anchor.
// Selecting anchors before joining labels/status keeps reads bounded by the
// requested page. The attachment/id and transfer/phase indexes already exist.
const attachmentDownloadTransfersSQL = `WITH recent AS MATERIALIZED (
 SELECT d.* FROM source_attachment_downloads d
 WHERE d.attachment_uuid=? AND d.id<? AND NOT EXISTS (
  SELECT 1 FROM source_attachment_downloads p
  WHERE p.producer_uuid=d.producer_uuid AND p.capture_event_uuid=d.capture_event_uuid
  AND p.attachment_uuid=d.attachment_uuid AND p.phase=1-d.phase AND p.id<d.id
 ) ORDER BY d.id DESC LIMIT ?
)
SELECT d.id,d.attachment_uuid,d.producer_uuid,d.run_uuid,d.fence,d.transfer_sequence,d.capture_event_uuid,
 r.collection_uuid,r.collection_revision,c.label AS collection_label,r.root_uuid,m.label AS root_label,
 coalesce(t.state,'started') AS reported_state,coalesce(t.reason_code,'') AS reason_code,
 CASE WHEN t.id IS NOT NULL THEN t.state WHEN r.state='running' AND r.fence=d.fence AND r.producer_uuid=d.producer_uuid
 AND r.owner_uuid=d.owner_uuid AND r.lease_until_ms>? THEN 'downloading' ELSE 'interrupted' END AS current_state,
 coalesce(s.event_uuid,'') AS start_event_uuid,coalesce(t.event_uuid,'') AS terminal_event_uuid,
 s.observed_at AS started_at,t.observed_at AS finished_at,d.recorded_at AS first_recorded_at,
 coalesce(s.id,0) AS start_id,coalesce(t.id,0) AS terminal_id,
 s.recorded_at AS start_recorded_at,t.recorded_at AS terminal_recorded_at,
 coalesce(t.file_event_uuid,'') AS file_event_uuid,coalesce(j.uuid,'') AS job_uuid,coalesce(j.state,'') AS job_state
FROM recent d JOIN source_runs r ON r.uuid=d.run_uuid
 JOIN source_collection_revisions c ON c.collection_uuid=r.collection_uuid AND c.revision=r.collection_revision
 JOIN media_roots b ON b.uuid=r.root_uuid JOIN media_root_revisions m ON m.root_uuid=b.uuid AND m.revision=b.revision
 LEFT JOIN source_attachment_downloads s ON s.producer_uuid=d.producer_uuid AND s.capture_event_uuid=d.capture_event_uuid
 AND s.attachment_uuid=d.attachment_uuid AND s.phase=0
 LEFT JOIN source_attachment_downloads t ON t.producer_uuid=d.producer_uuid AND t.capture_event_uuid=d.capture_event_uuid
 AND t.attachment_uuid=d.attachment_uuid AND t.phase=1
 LEFT JOIN ingest_receipts f ON f.producer_uuid=t.producer_uuid AND f.event_uuid=t.file_event_uuid
 LEFT JOIN archive_jobs j ON j.uuid=f.job_uuid
ORDER BY d.id DESC`

func (s *SourceAttachmentStore) DownloadTransfers(ctx context.Context, id string, before int64, limit int, now time.Time) (*models.AttachmentDownloadPage, error) {
	if _, err := archiveUUID(id); err != nil || before < 0 || before > 9007199254740991 || limit < 1 || limit > 25 || now.IsZero() {
		return nil, models.ErrAttachmentDownloadInvalid
	}
	if before == 0 {
		before = math.MaxInt64
	}
	var rows []struct {
		ID              int64      `db:"id"`
		Attachment      string     `db:"attachment_uuid"`
		Producer        string     `db:"producer_uuid"`
		Run             string     `db:"run_uuid"`
		Fence           int64      `db:"fence"`
		Transfer        int64      `db:"transfer_sequence"`
		Capture         string     `db:"capture_event_uuid"`
		Collection      string     `db:"collection_uuid"`
		Revision        int        `db:"collection_revision"`
		CollectionLabel string     `db:"collection_label"`
		Root            string     `db:"root_uuid"`
		RootLabel       string     `db:"root_label"`
		Reported        string     `db:"reported_state"`
		Reason          string     `db:"reason_code"`
		State           string     `db:"current_state"`
		Start           string     `db:"start_event_uuid"`
		End             string     `db:"terminal_event_uuid"`
		Started         *Timestamp `db:"started_at"`
		Finished        *Timestamp `db:"finished_at"`
		First           Timestamp  `db:"first_recorded_at"`
		StartID         int64      `db:"start_id"`
		TerminalID      int64      `db:"terminal_id"`
		StartRecorded   *Timestamp `db:"start_recorded_at"`
		EndRecorded     *Timestamp `db:"terminal_recorded_at"`
		File            string     `db:"file_event_uuid"`
		Job             string     `db:"job_uuid"`
		JobState        string     `db:"job_state"`
	}
	if err := dbWrapper.Select(ctx, &rows, attachmentDownloadTransfersSQL, id, before, limit+1, now.UnixMilli()); err != nil {
		return nil, err
	}
	result := &models.AttachmentDownloadPage{AttachmentUUID: id, CheckedAt: now, Transfers: []models.AttachmentDownloadTransfer{}}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		result.NextBefore = &cursor
		rows = rows[:limit]
	}
	for _, row := range rows {
		item := models.AttachmentDownloadTransfer{Sequence: row.ID, AttachmentUUID: row.Attachment, ProducerUUID: row.Producer,
			RunUUID: row.Run, Fence: row.Fence, TransferSequence: row.Transfer, CaptureEventUUID: row.Capture, CollectionUUID: row.Collection,
			CollectionRevision: row.Revision, CollectionLabel: row.CollectionLabel, RootUUID: row.Root, RootLabel: row.RootLabel,
			State: row.State, ReportedState: row.Reported, ReasonCode: row.Reason, StartEventUUID: row.Start, TerminalEventUUID: row.End,
			FirstRecordedAt: row.First.Timestamp, LastRecordedAt: row.First.Timestamp, FileEventUUID: row.File,
			VerificationJob: row.Job, VerificationState: row.JobState}
		if row.Started != nil {
			item.StartedAt = &row.Started.Timestamp
		}
		if row.Finished != nil {
			item.FinishedAt = &row.Finished.Timestamp
		}
		if row.StartID > row.TerminalID && row.StartRecorded != nil {
			item.LastRecordedAt = row.StartRecorded.Timestamp
		} else if row.EndRecorded != nil {
			item.LastRecordedAt = row.EndRecorded.Timestamp
		}
		result.Transfers = append(result.Transfers, item)
	}
	return result, nil
}
