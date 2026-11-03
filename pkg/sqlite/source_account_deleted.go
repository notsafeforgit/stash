package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

// A trusted, fenced worker reports the explicit provider deletion message.
// Generic missing profiles and errors from external media cannot retire a source.
func deletedRedgifsCollection(ctx context.Context, r *models.SourceRun, outcome models.SourceRunOutcome) (*models.SourceCollection, error) {
	if outcome.State != "deferred" || outcome.ErrorScope != "service:redgifs" || outcome.RetryAfterSeconds != 0 || r.Operation != "download" || r.AttachmentRetry {
		return nil, models.ErrSourceRunInvalid
	}
	c, _, err := sourceRunDefinition(ctx, r)
	if err != nil {
		return nil, err
	}
	if c.Kind != "account" || c.Namespace != "native:redgifs" || scrape.RedgifsProfileURL(c.TargetURL) == "" || scrape.RedgifsProfileURL(c.TargetURL) != scrape.RedgifsProfileURL(r.TargetURL) {
		return nil, models.ErrSourceRunInvalid
	}
	var listing bool
	if err := dbWrapper.Get(ctx, &listing, `SELECT EXISTS(SELECT 1 FROM source_request_permits
 WHERE run_uuid=? AND fence=? AND scope='service:redgifs' AND operation='listing' AND reported=1 AND granted_at_ms IS NOT NULL)`, r.UUID, r.Fence); err != nil {
		return nil, err
	}
	if !listing {
		return nil, models.ErrSourceRunInvalid
	}
	return c, nil
}

func retireDeletedRedgifsCollection(ctx context.Context, r *models.SourceRun, c *models.SourceCollection, now time.Time) error {
	definition := c.SourceCollectionDefinition
	definition.State = "retired"
	if _, err := (&SourceCollectionStore{}).Put(ctx, models.SourceCollectionInput{
		UUID: c.UUID, ExpectedRevision: c.Revision, SourceCollectionDefinition: definition,
		Origin: "ingest", Reason: fmt.Sprintf("Redgifs explicitly confirmed account deletion in source run %s, attempt %d", r.UUID, r.Fence),
	}); err != nil {
		return err
	}
	// Close work before disabling the subscription, so previously cancelled
	// historical runs keep their original reason for cancellation.
	if _, err := dbWrapper.Exec(ctx, `UPDATE source_runs SET state='cancelled',error_code='account_deleted',revision=revision+1,updated_at_ms=?
 WHERE collection_uuid=? AND state IN ('queued','deferred')`, now.UnixMilli(), c.UUID); err != nil {
		return err
	}
	subscriptions := &SourceSubscriptionStore{}
	subscription, err := subscriptions.Find(ctx, c.UUID)
	if err != nil {
		return err
	}
	if subscription.Enabled {
		if _, err := subscriptions.Put(ctx, c.UUID, models.SourceSubscriptionInput{ExpectedRevision: subscription.Revision, Enabled: false}); err != nil {
			return err
		}
	}
	// Preserve requests, pending ranges, captures and completed media. Initial
	// and manual work for this deleted Redgifs account cannot recover either.
	return nil
}
