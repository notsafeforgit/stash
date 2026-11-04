package ingest_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func hostedCaptureEvent(t *testing.T, f intakePublicationFixture, raw string, ref ingest.PostReference) ingest.CaptureEvent {
	t.Helper()
	event := f.event(t)
	event.RootUUID, event.Post = &f.root.UUID, ref
	var err error
	event.Source, err = archive.RetainSourcePayload([]byte(raw))
	require.NoError(t, err)
	event.Metadata, err = archive.CapturedMetadata(event.Source)
	require.NoError(t, err)
	return event
}

func TestRedditCaptureReusesHostedFileWithoutDownloadAndRollsBack(t *testing.T) {
	for _, scenario := range []string{"archive-skip", "stale-file", "explicit-unlink"} {
		t.Run(scenario, func(t *testing.T) {
			f := newIntakePublicationFixture(t, false)
			repo := f.service.Repo
			var err error
			f.credential, f.token, err = f.service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID, RootUUID: &f.root.UUID}}, nil)
			require.NoError(t, err)
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				policy, err := repo.MetadataPolicy.Put(ctx, models.MetadataPolicyInput{CollectionUUID: f.collection.UUID,
					ExpectedCollectionRevision: f.collection.Revision, Origin: "review", Definition: models.MetadataPolicyDefinition{
						Enabled: true, Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{models.ArchiveImage: {
							OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{
								"title":   {JQ: ".source.metadata.title // .source.metadata.original_text"},
								"details": {JQ: ".source.metadata.original_text"}, "date": {JQ: ".source.metadata.published_at[:10]"},
							}},
						}},
				})
				if err == nil {
					f.input.PolicyRevision = policy.Revision
				}
				return err
			}))
			hosted := hostedCaptureEvent(t, f, `{"category":"redgifs","id":"HostedClip","description":"Host caption","createDate":1704067200,"urls":{"hd":"https://media.redgifs.com/HostedClip.mp4"}}`, ingest.PostReference{Namespace: "native:redgifs", Value: "hostedclip"})
			f.receipt, err = f.submit(t, hosted)
			require.NoError(t, err)
			f.input.Source = &ingest.IntakeSource{CaptureUUID: f.receipt.CaptureUUID, AttachmentUUID: f.attachment(t, 0).UUID}
			published := f.publishIntake(t, f.prepared, f.input)
			if scenario == "stale-file" {
				require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
					_, err := repo.FileContent.Advance(ctx, published.Result.FileUUID, published.Result.Generation)
					return err
				}))
			}
			if scenario == "explicit-unlink" {
				attachment := f.attachment(t, 0)
				require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
					_, err := repo.SourceAttachment.DecideMedia(ctx, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID,
						ExpectedAttachmentRevision: attachment.Revision, State: "unlinked", Origin: "review"})
					return err
				}))
			}
			incoming := hostedCaptureEvent(t, f, `{"category":"reddit","id":"parent1","title":"Reddit title","selftext":"Reddit description","date":"2024-02-01T00:00:00Z","url":"https://www.redgifs.com/watch/HostedClip"}`, ingest.PostReference{Namespace: "native:reddit", Value: "parent1"})
			if scenario == "archive-skip" {
				store := repo.Ingest
				f.service.Repo.Ingest = failingReceiptStore{store}
				_, err := f.submit(t, incoming)
				require.ErrorContains(t, err, "receipt storage failed")
				f.service.Repo.Ingest = store
				require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
					state, err := repo.MetadataField.State(ctx, published.Result.MediaUUID, "title")
					require.JSONEq(t, `"Host caption"`, string(state.Value), "metadata rolls back with the capture receipt")
					return err
				}))
			}
			f.receipt, err = f.submit(t, incoming)
			require.NoError(t, err)
			attachment := f.attachment(t, 0)
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				choice, err := repo.SourceAttachment.MediaDecision(ctx, attachment.UUID)
				require.NoError(t, err)
				if scenario != "archive-skip" {
					require.Nil(t, choice)
					return nil
				}
				require.NotNil(t, choice)
				require.Equal(t, published.Result.MediaUUID, *choice.MediaUUID)
				for field, value := range map[string]string{"title": `"Reddit title"`, "details": `"Reddit description"`, "date": `"2024-02-01"`} {
					state, err := repo.MetadataField.State(ctx, published.Result.MediaUUID, field)
					require.NoError(t, err)
					require.JSONEq(t, value, string(state.Value))
				}
				return nil
			}))
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
			replay, err := f.submit(t, incoming)
			require.NoError(t, err)
			require.Equal(t, f.receipt, replay)
		})
	}
}

func TestExistingHostedMediaJoinsRedditGallery(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	standalone := f.input
	standalone.Source = nil
	first := f.publishIntake(t, f.prepared, standalone)
	other := f.linkSecondImage(t)
	// The existing file's identity is reused when its Reddit gallery arrives.
	second := f.publishIntake(t, f.prepared, f.input)
	require.False(t, second.Result.MediaCreated)
	require.Equal(t, first.Result.MediaUUID, second.Result.MediaUUID)
	require.NotEmpty(t, second.Result.GalleryUUID)
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		gallery, err := f.service.Repo.ArchiveEntity.Find(ctx, second.Result.GalleryUUID)
		require.NoError(t, err)
		members, err := f.service.Repo.Gallery.GetImageIDs(ctx, *gallery.LocalID)
		require.NoError(t, err)
		require.ElementsMatch(t, []int{*first.Media.Media.LocalID, other}, members)
		return nil
	}))
}
