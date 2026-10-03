package ingest

import (
	"context"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// Publish consumes only the saved checkpoint. The caller cannot replace source
// bytes, supply capture identities or turn a worker's success flag into proof.
func (c *EnrichmentCoordinator) Publish(ctx context.Context, token string, lease models.ArchiveJobLease, expected int, digest string) (*models.EnrichmentPublication, error) {
	var result *models.EnrichmentPublication
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, _, work, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		owned := models.EnrichmentJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}
		prior, err := c.Service.Repo.EnrichmentJob.Publication(ctx, lease.JobUUID)
		if err != nil {
			return err
		}
		if prior != nil {
			result, err = c.Service.Repo.EnrichmentJob.Publish(ctx, owned, expected, digest, nil, c.Now())
			if err != nil {
				return err
			}
			if _, err := c.Service.Repo.EnrichmentJob.ReleaseCheckpoint(ctx, lease.JobUUID, c.Now()); err != nil {
				return err
			}
			current, err := c.Service.Repo.ArchiveJob.Find(ctx, lease.JobUUID)
			if err != nil {
				return err
			}
			c.guard(ctx, token, current, nil)
			return nil
		}
		before, err := c.Service.Repo.EnrichmentJob.CheckLease(ctx, owned, c.Now())
		if err != nil {
			return err
		}
		head, err := c.Service.Repo.EnrichmentJob.CheckpointHead(ctx, lease.JobUUID)
		if err != nil {
			return err
		}
		if head == nil || head.Revision != expected || head.Digest != digest || head.PendingCount != 0 {
			return models.ErrEnrichmentConflict
		}
		transcript, err := archive.ParseEnrichmentTranscript(head.Body)
		if err != nil {
			return err
		}
		collection, err := c.Service.Repo.SourceCollection.Find(ctx, work.CollectionUUID)
		if err != nil {
			return err
		}
		captures := make([]string, len(transcript.Records))
		seen := make(map[string]bool)
		for after := -1; after+1 < len(captures); {
			records, err := c.Service.Repo.EnrichmentJob.CheckpointRecords(ctx, lease.JobUUID, after, 100)
			if err != nil {
				return err
			}
			if len(records) == 0 {
				return models.ErrSourcePayloadCorrupt
			}
			for _, record := range records {
				if !c.Now().Before(*before.LeaseUntil) {
					return models.ErrArchiveJobLease
				}
				if record.Ordinal != after+1 || record.Ordinal >= len(captures) {
					return models.ErrSourcePayloadCorrupt
				}
				var parent string
				if ordinal := transcript.Records[record.Ordinal].Parent; ordinal != nil {
					parent = captures[*ordinal]
				}
				var retained *models.SourceCapture
				if record.RetainedCapture != nil {
					retained, err = c.Service.Repo.SourceEvidence.FindCapture(ctx, *record.RetainedCapture)
					if err != nil {
						return err
					}
				}
				input, reference, err := archive.PrepareEnrichmentRecord(lease.JobUUID, work, transcript, record, parent, retained)
				if err != nil {
					return err
				}
				post, err := c.Service.Repo.SourceEvidence.FindPostByIdentifier(ctx, *reference)
				if err != nil {
					return err
				}
				if post == nil || post.UUID != work.PostUUID || post.State != "active" ||
					(collection.Namespace != "" && collection.Namespace != reference.Namespace) {
					return models.ErrEnrichmentConflict
				}
				if retained != nil {
					// This association reuses an original observation. Do not run
					// publisher or translation assessment as a new scrape.
					if err := c.Service.Repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: work.CollectionUUID, CollectionRevision: work.CollectionRevision, CaptureUUID: input.UUID, CreatedAt: c.Now()}); err != nil {
						return err
					}
				} else if !seen[input.UUID] {
					raw, err := transcript.Metadata(record.Ordinal)
					if err != nil {
						return err
					}
					album, err := archive.ExtractCapturedAlbum(raw)
					if err != nil || (album != nil && album.Post != *reference) {
						return ErrInvalid
					}
					if _, _, err := c.Service.recordPreparedCapture(ctx, *input, album, collection, c.Now()); err != nil {
						return err
					}
					seen[input.UUID] = true
				}
				captures[record.Ordinal], after = input.UUID, record.Ordinal
			}
		}
		result, err = c.Service.Repo.EnrichmentJob.Publish(ctx, owned, expected, digest, captures, c.Now())
		if err != nil {
			return err
		}
		if _, err := c.Service.Repo.EnrichmentJob.ReleaseCheckpoint(ctx, lease.JobUUID, c.Now()); err != nil {
			return err
		}
		after, err := c.Service.Repo.ArchiveJob.Find(ctx, lease.JobUUID)
		if err != nil {
			return err
		}
		c.guard(ctx, token, after, nil)
		// The generic job and target are terminal now, so checking a current
		// running lease would be incorrect. Preserve its pre-publication deadline.
		txn.AddPreCommitHook(ctx, func(context.Context) error {
			if !c.Now().Before(*before.LeaseUntil) {
				return models.ErrArchiveJobLease
			}
			return nil
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *EnrichmentCoordinator) Publication(ctx context.Context, token, id string) (*models.EnrichmentPublication, error) {
	var result *models.EnrichmentPublication
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		if _, _, _, err := c.allowed(ctx, token, id); err != nil {
			return err
		}
		var err error
		result, err = c.Service.Repo.EnrichmentJob.Publication(ctx, id)
		return err
	})
	return result, err
}

func (c *EnrichmentCoordinator) CheckpointRelease(ctx context.Context, token, id string) (*models.EnrichmentCheckpointRelease, error) {
	var result *models.EnrichmentCheckpointRelease
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		if _, _, _, err := c.allowed(ctx, token, id); err != nil {
			return err
		}
		var err error
		result, err = c.Service.Repo.EnrichmentJob.CheckpointRelease(ctx, id)
		return err
	})
	return result, err
}

// Older verified publications retain staging through migration. Cleanup can
// release them without a new extraction or ownership of an expired worker lease.
func (c *EnrichmentCoordinator) ReleaseCheckpoint(ctx context.Context, token, id string) (*models.EnrichmentCheckpointRelease, error) {
	var result *models.EnrichmentCheckpointRelease
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		_, job, _, err := c.allowed(ctx, token, id)
		if err != nil {
			return err
		}
		result, err = c.Service.Repo.EnrichmentJob.ReleaseCheckpoint(ctx, id, c.Now())
		if err != nil {
			return err
		}
		c.guard(ctx, token, job, nil)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
