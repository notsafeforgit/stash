package ingest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func (f intakePublicationFixture) policy(t *testing.T, revision int, mappings map[string]models.MetadataMapping) *models.MetadataPolicy {
	t.Helper()
	var ret *models.MetadataPolicy
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = f.service.Repo.MetadataPolicy.Put(ctx, models.MetadataPolicyInput{CollectionUUID: f.collection.UUID, ExpectedCollectionRevision: f.collection.Revision, ExpectedRevision: revision, Origin: "review",
			Definition: models.MetadataPolicyDefinition{Enabled: true, Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{models.ArchiveImage: {OnCreate: true, OnExisting: true, FilenameTitleFallback: true, Mappings: mappings}}}})
		return err
	}))
	return ret
}

func intakeField(t *testing.T, repo models.Repository, entity, field string) *models.MetadataFieldState {
	t.Helper()
	var ret *models.MetadataFieldState
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.MetadataField.State(ctx, entity, field)
		return err
	}))
	return ret
}

func TestIntakeMetadataPolicyUsesCaptureAndPreservesSourceAlternatives(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	repo := f.service.Repo
	policy := f.policy(t, 0, map[string]models.MetadataMapping{"title": {JQ: `.source.metadata.title // empty`}})
	f.input.PolicyRevision = policy.Revision
	first := f.publishIntake(t, f.prepared, f.input)
	state := intakeField(t, repo, first.Result.MediaUUID, "title")
	require.JSONEq(t, `"Source album"`, string(state.Value))
	require.Equal(t, "source", state.Origin)
	require.Equal(t, f.receipt.CaptureUUID, *state.Decision.CaptureUUID)
	require.Equal(t, policy.MetadataPolicyRef, *state.Decision.Policy)
	require.Equal(t, "[]", string(intakeField(t, repo, first.Result.MediaUUID, "performers").Value), "publisher identity must never imply depicted performer attribution")
	// A later scrape of the same post can update inherited fields.
	event := f.event(t)
	event.RootUUID = &f.root.UUID
	event.ObservedAt = time.Now().UTC().Add(time.Second)
	title := "Updated caption"
	event.Metadata.Title = &title
	next, err := f.submit(t, event)
	require.NoError(t, err)
	input := f.input
	input.UUID = uuid.NewString()
	input.Source = &ingest.IntakeSource{CaptureUUID: next.CaptureUUID, AttachmentUUID: f.input.Source.AttachmentUUID}
	updated := f.publishIntake(t, f.prepared, input)
	require.Equal(t, first.Result.MediaUUID, updated.Result.MediaUUID)
	require.Equal(t, `"Updated caption"`, string(intakeField(t, repo, first.Result.MediaUUID, "title").Value))
	// An out-of-order completion with the older capture does not roll it back.
	older := f.publishIntake(t, f.prepared, f.input)
	require.Empty(t, older.Result.MetadataFields)
	require.Equal(t, `"Updated caption"`, string(intakeField(t, repo, first.Result.MediaUUID, "title").Value))
	// The same bytes in another post retain another source appearance, without
	// replacing the chosen caption merely because that download finished last.
	event.EventUUID = uuid.NewString()
	event.Post.Value = "post2"
	event.Source = bytes.ReplaceAll(event.Source, []byte(`"post1"`), []byte(`"post2"`))
	title = "Another post"
	event.Metadata.Title = &title
	event.ObservedAt = event.ObservedAt.Add(time.Second)
	f.receipt, err = f.submit(t, event)
	require.NoError(t, err)
	input.UUID = uuid.NewString()
	input.Source = &ingest.IntakeSource{CaptureUUID: f.receipt.CaptureUUID, AttachmentUUID: f.attachment(t, 0).UUID}
	competing := f.publishIntake(t, f.prepared, input)
	require.Contains(t, competing.Result.Review, "metadata:title")
	require.Equal(t, `"Updated caption"`, string(intakeField(t, repo, first.Result.MediaUUID, "title").Value))
	// Removing the source association prevents using that capture in a preview.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		attachment, err := repo.SourceAttachment.Find(ctx, input.Source.AttachmentUUID)
		if err != nil {
			return err
		}
		_, err = repo.SourceAttachment.DecideMedia(ctx, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision, State: "unlinked", Origin: "review"})
		return err
	}))
	err = repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := (metadata.Service{Repo: repo}).Preview(ctx, metadata.Input{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, PolicyRevision: policy.Revision, EntityUUID: first.Result.MediaUUID, RelativePath: input.Target.RelativePath, Source: &metadata.Source{CaptureUUID: input.Source.CaptureUUID, AttachmentUUID: input.Source.AttachmentUUID}})
		return err
	})
	require.ErrorIs(t, err, models.ErrMetadataPolicyConflict)
}

func TestFileWorkerPinsPolicyAndReportsChangedPolicyWithoutLosingMedia(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	policy := f.policy(t, 0, map[string]models.MetadataMapping{"title": {JQ: `.source.metadata.title // empty`}})
	event := f.fileEvent(t)
	_, err := f.submitFile(t, event)
	require.NoError(t, err)
	f.policy(t, policy.Revision, map[string]models.MetadataMapping{"title": {Value: json.RawMessage(`"Changed policy"`)}})
	worker := f.worker(t, func(context.Context, ingest.FileWork, ingest.IntakePublicationResult, ingest.FileEffectGuard) error {
		return nil
	})
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	status, result := f.fileStatus(t, event.EventUUID)
	require.Equal(t, "succeeded", status.State)
	require.True(t, result.MediaIngested)
	require.Equal(t, "policy_changed", result.Publication.MetadataState)
	require.Contains(t, result.Publication.Review, "metadata:policy_changed")
	require.Equal(t, `""`, string(intakeField(t, f.service.Repo, result.Publication.MediaUUID, "title").Value))
	// A newly admitted event pins and applies the now-current definition.
	event.EventUUID = uuid.NewString()
	_, err = f.submitFile(t, event)
	require.NoError(t, err)
	_, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	_, result = f.fileStatus(t, event.EventUUID)
	require.Equal(t, `"Changed policy"`, string(intakeField(t, f.service.Repo, result.Publication.MediaUUID, "title").Value))
}
