package ingest_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestRedditExternalCapturesShareAttachmentAndPublishMedia(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	repo := f.service.Repo
	var attachmentUUID, publisherUUID string
	for _, source := range []string{
		`{"category":"redgifs","id":"linkedclip","userName":"host-account","urls":{"hd":"https://media.redgifs.com/LinkedClip.png"},"_url":"https://media.redgifs.com/LinkedClip.png","_reddit":{"category":"reddit","id":"externalpost","author":"publisher","author_fullname":"t2_123","url":"https://www.redgifs.com/watch/LinkedClip"}}`,
		`{"category":"reddit","id":"externalpost","author":"publisher","author_fullname":"t2_123","url":"https://www.redgifs.com/watch/LinkedClip","_url":"https://external-preview.redd.it/preview.png","preview":{"images":[{"source":{"url":"https://external-preview.redd.it/preview.png"}}]}}`,
	} {
		capture := f.event(t)
		capture.RootUUID = &f.root.UUID
		capture.Post.Value = "externalpost"
		capture.Source = json.RawMessage(source)
		var err error
		f.receipt, err = f.submit(t, capture)
		require.NoError(t, err)
		replay, err := f.submit(t, capture)
		require.NoError(t, err)
		require.Equal(t, f.receipt, replay)
		require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			selection, err := repo.SourceAttachment.Selection(ctx, f.receipt.PostUUID)
			require.NoError(t, err)
			require.Len(t, selection.Entries, 1)
			require.False(t, selection.Complete, "a linked clip does not prove external gallery membership")
			require.Nil(t, selection.ExpectedCount)
			attachment := selection.Entries[0].Attachment
			require.Equal(t, models.SourcePostIdentifier{Namespace: "native:redgifs", Value: "linkedclip"}, attachment.Reference)
			publisher, err := repo.CapturePublisher.Current(ctx, f.receipt.CaptureUUID)
			require.NoError(t, err)
			require.NotNil(t, publisher.AccountUUID)
			if attachmentUUID == "" {
				attachmentUUID, publisherUUID = attachment.UUID, *publisher.AccountUUID
			} else {
				require.Equal(t, attachmentUUID, attachment.UUID)
				require.Equal(t, publisherUUID, *publisher.AccountUUID, "the hosting service does not replace the Reddit publisher")
			}
			return nil
		}))

		event := f.fileEvent(t)
		accepted, err := f.submitFile(t, event)
		require.NoError(t, err)
		durable := job.NewDurable(repo)
		claimed, err := durable.Claim(t.Context(), models.ArchiveJobVerifyMedia, uuid.NewString(), time.Minute)
		require.NoError(t, err)
		require.Equal(t, accepted.JobUUID, claimed.UUID)
		var work ingest.FileWork
		require.NoError(t, json.Unmarshal(claimed.Arguments, &work))
		_, err = durable.Publish(t.Context(), claimed.Lease(), func(ctx context.Context, _ *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
			published, err := f.prepared.PublishIntake(ctx, repo, work.Publication)
			if err != nil {
				return models.ArchiveJobOutcome{}, err
			}
			require.Equal(t, "linked", published.Result.SourceMedia)
			require.NotEmpty(t, published.Result.MediaUUID)
			require.Empty(t, published.Result.GalleryUUID)
			body, err := json.Marshal(published.Result)
			return models.ArchiveJobOutcome{State: "succeeded", Result: body}, err
		})
		require.NoError(t, err)
		status, err := f.service.ReceiptStatus(t.Context(), f.token, event.EventUUID)
		require.NoError(t, err)
		require.Equal(t, "succeeded", status.State)
	}
}
