package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

var retryRedditPostID = regexp.MustCompile(`^[a-z0-9]{1,32}$`)

const attachmentRetryLifetime = 48 * time.Hour

func attachmentRetryFailure(code string) bool {
	return code == "source_download_failed" || code == "not_found"
}

// At expiry, the remaining work is a durable local archive write. Its lease
// survives crashes, but cannot authorize another attempt to fetch the post.
func (s *SourceRunStore) prepareAttachmentRetryFinalization(ctx context.Context, run *models.SourceRun, now time.Time) error {
	if !run.AttachmentRetry || run.AttachmentRetryFinalize || !attachmentRetryFailure(run.ErrorCode) || now.Before(run.CreatedAt.Add(attachmentRetryLifetime)) {
		return nil
	}
	_, err := dbWrapper.Exec(ctx, "UPDATE source_run_attachment_retries SET finalize=1 WHERE run_uuid=?", run.UUID)
	run.AttachmentRetryFinalize = err == nil
	return err
}

// Only an owned retry claim receives the original retained source, so its
// archive path can be resolved without fetching the post again. List responses
// and subsequent renewals do not duplicate that payload.
func attachmentRetryFinalizationSource(ctx context.Context, run *models.SourceRun) error {
	if run == nil || !run.AttachmentRetry {
		return nil
	}
	var captureID string
	err := dbWrapper.Get(ctx, &captureID, `SELECT receipt.capture_uuid FROM source_run_attachment_retries retry
 JOIN source_attachment_downloads d ON d.producer_uuid=retry.producer_uuid AND d.event_uuid=retry.event_uuid
 JOIN ingest_receipts receipt ON receipt.producer_uuid=d.producer_uuid AND receipt.event_uuid=d.capture_event_uuid
 WHERE retry.run_uuid=?`, run.UUID)
	if err != nil {
		return err
	}
	capture, err := (&SourceEvidenceStore{}).FindCapture(ctx, captureID)
	if err != nil {
		return err
	}
	if capture == nil {
		return models.ErrSourcePayloadCorrupt
	}
	run.AttachmentRetrySource, err = archive.RestoreCapture(capture.Payload)
	return err
}

// An ordinary listing must not be held open by a missing file. Its failed
// transfer is retained first; only that producer's receipt can admit a focused
// retry at the same root, collection revision and original traversal window.
func validateSourceAttachmentRetry(ctx context.Context, producer string, input models.SourceRunRequest) error {
	if !validSourceRunUUID(input.AttachmentRetryEventUUID) || input.Operation != "download" {
		return models.ErrSourceRunInvalid
	}
	var row struct {
		State        string `db:"state"`
		Collection   string `db:"collection_uuid"`
		Revision     int    `db:"collection_revision"`
		Window       string `db:"window"`
		Post         string `db:"post_uuid"`
		Subscription int    `db:"subscription_revision"`
		RootCurrent  bool   `db:"root_current"`
	}
	err := dbWrapper.Get(ctx, &row, `SELECT d.state,r.collection_uuid,r.collection_revision,a.window,item.post_uuid,
 EXISTS(SELECT 1 FROM media_roots root WHERE root.uuid=r.root_uuid AND root.revision=r.root_revision) AS root_current,
 coalesce((SELECT revision FROM source_run_subscriptions WHERE run_uuid=r.uuid),0) AS subscription_revision
 FROM source_attachment_downloads d JOIN source_runs r ON r.uuid=d.run_uuid
 JOIN source_run_attempts a ON a.run_uuid=d.run_uuid AND a.fence=d.fence
 JOIN source_attachments item ON item.uuid=d.attachment_uuid
 WHERE d.producer_uuid=? AND d.event_uuid=?`, producer, input.AttachmentRetryEventUUID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrSourceRunDependency
	}
	if err != nil {
		return err
	}
	if row.State != "failed" || row.Collection != input.CollectionUUID || row.Revision != input.CollectionRevision || row.Subscription != input.SubscriptionRevision || !row.RootCurrent {
		return models.ErrSourceRunInvalid
	}
	var window models.SourceWindow
	if err := json.Unmarshal([]byte(row.Window), &window); err != nil {
		return err
	}
	if !scrape.SameWindow(window, input.Window) {
		return models.ErrSourceRunInvalid
	}
	var identifiers []string
	if err := dbWrapper.Select(ctx, &identifiers, "SELECT value FROM source_post_identifiers WHERE post_uuid=? AND namespace='native:reddit' LIMIT 2", row.Post); err != nil {
		return err
	}
	if len(identifiers) != 1 || !retryRedditPostID.MatchString(identifiers[0]) || input.RetrievalURL != "https://www.reddit.com/comments/"+identifiers[0]+"/" {
		return models.ErrSourceRunInvalid
	}
	return nil
}
