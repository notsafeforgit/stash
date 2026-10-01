package ingest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/file"
	imagefile "github.com/stashapp/stash/pkg/file/image"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

type intakePublicationFixture struct {
	publicationFixture
	receipt *models.IngestReceipt
	input   ingest.IntakePublication
}

func newIntakePublicationFixture(t *testing.T, source bool) intakePublicationFixture {
	t.Helper()
	f := intakePublicationFixture{publicationFixture: newPublicationFixture(t)}
	repo := f.service.Repo
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		definition := f.collection.SourceCollectionDefinition
		definition.RootUUID, definition.PathPrefix = &f.root.UUID, "."
		if !source {
			definition.Kind, definition.Namespace = "manual_batch", ""
		}
		var err error
		f.collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{
			UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review",
		})
		return err
	}))
	f.input = ingest.IntakePublication{UUID: uuid.NewString(), CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, Target: *f.target, Kind: models.ArchiveImage}
	if source {
		var err error
		f.credential, f.token, err = f.service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID, RootUUID: &f.root.UUID}}, nil)
		require.NoError(t, err)
		event := f.event(t)
		event.RootUUID = &f.root.UUID
		f.receipt, err = f.submit(t, event)
		require.NoError(t, err)
		f.input.Source = &ingest.IntakeSource{CaptureUUID: f.receipt.CaptureUUID, AttachmentUUID: f.attachment(t, 0).UUID}
	}
	return f
}

func (f intakePublicationFixture) attachment(t *testing.T, index int) models.SourceAttachment {
	t.Helper()
	var ret models.SourceAttachment
	repo := f.service.Repo
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		selection, err := repo.SourceAttachment.Selection(ctx, f.receipt.PostUUID)
		if err != nil {
			return err
		}
		ret = selection.Entries[index].Attachment
		return nil
	}))
	return ret
}

func (f intakePublicationFixture) publishIntake(t *testing.T, prepared *ingest.PreparedMedia, input ingest.IntakePublication) *ingest.PublishedIntake {
	t.Helper()
	repo := f.service.Repo
	var ret *ingest.PublishedIntake
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = prepared.PublishIntake(ctx, repo, input)
		return err
	}))
	return ret
}

func TestIntakePublicationBuildsAlbumAsFilesArriveAndReplays(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	repo := f.service.Repo
	first := f.publishIntake(t, f.prepared, f.input)
	require.Equal(t, "linked", first.Result.SourceMedia)
	require.NotEmpty(t, first.Result.GalleryUUID)
	replay := f.publishIntake(t, f.prepared, f.input)
	require.Equal(t, first.Result.MediaUUID, replay.Result.MediaUUID)
	require.Equal(t, first.Result.GalleryUUID, replay.Result.GalleryUUID)
	require.False(t, replay.Result.MediaCreated)
	// A distinct second image joins the same source album; download order and
	// pathname prefixes do not decide gallery identity.
	var body bytes.Buffer
	require.NoError(t, png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 19, 29))))
	path := filepath.Join(f.root.Binding.Path, "second.png")
	require.NoError(t, os.WriteFile(path, body.Bytes(), 0600))
	scanner := &file.Scanner{FingerprintCalculator: intakeFingerprinter{}, FileDecorators: []file.Decorator{&imagefile.Decorator{FFProbe: intakeProbe(t)}}}
	prepared, err := ingest.PrepareMedia(t.Context(), *f.root, "second.png", nil, "", scanner)
	require.NoError(t, err)
	defer prepared.Close()
	secondInput := f.input
	secondInput.UUID, secondInput.Target = uuid.NewString(), *f.captureTarget(t, "second.png", true)
	secondInput.Source = &ingest.IntakeSource{CaptureUUID: f.receipt.CaptureUUID, AttachmentUUID: f.attachment(t, 1).UUID}
	second := f.publishIntake(t, prepared, secondInput)
	require.NotEqual(t, first.Result.MediaUUID, second.Result.MediaUUID)
	require.Equal(t, first.Result.GalleryUUID, second.Result.GalleryUUID)
	require.Empty(t, second.Result.Review)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		gallery, err := repo.ArchiveEntity.Find(ctx, first.Result.GalleryUUID)
		require.NoError(t, err)
		members, err := repo.Gallery.GetImageIDs(ctx, *gallery.LocalID)
		require.NoError(t, err)
		require.ElementsMatch(t, []int{*first.Media.Media.LocalID, *second.Media.Media.LocalID}, members)
		intakes, err := repo.SourceCollection.MediaIntake(ctx, f.collection.UUID, "", 10)
		require.NoError(t, err)
		require.Len(t, intakes, 2)
		evidence, err := repo.SourceAttachment.MediaEvidence(ctx, f.input.Source.AttachmentUUID, "", 10)
		require.NoError(t, err)
		require.Len(t, evidence, 1)
		performers, err := repo.Image.GetPerformerIDs(ctx, *first.Media.Media.LocalID)
		require.NoError(t, err)
		require.Empty(t, performers, "a source publisher does not establish depicted performers")
		return nil
	}))
	// An explicit membership removal remains excluded after another delivery.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		gallery, err := repo.ArchiveEntity.Find(ctx, first.Result.GalleryUUID)
		if err != nil {
			return err
		}
		return repo.Gallery.RemoveImages(ctx, *gallery.LocalID, *first.Media.Media.LocalID)
	}))
	f.publishIntake(t, f.prepared, f.input)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		gallery, err := repo.ArchiveEntity.Find(ctx, first.Result.GalleryUUID)
		require.NoError(t, err)
		members, err := repo.Gallery.GetImageIDs(ctx, *gallery.LocalID)
		require.NoError(t, err)
		require.Equal(t, []int{*second.Media.Media.LocalID}, members)
		return nil
	}))
}

func TestIntakePublicationManualMediaNeedsNoScrapedIdentity(t *testing.T) {
	f := newIntakePublicationFixture(t, false)
	result := f.publishIntake(t, f.prepared, f.input)
	require.True(t, result.Result.MediaCreated)
	require.Equal(t, "not_applicable", result.Result.SourceMedia)
	require.Empty(t, result.Result.GalleryUUID)
	repo := f.service.Repo
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		intakes, err := repo.SourceCollection.MediaIntake(ctx, f.collection.UUID, "", 10)
		require.NoError(t, err)
		require.Len(t, intakes, 1)
		captures, err := repo.SourceCollection.Captures(ctx, f.collection.UUID, nil, 10)
		require.NoError(t, err)
		require.Empty(t, captures)
		return nil
	}))
}

func TestIntakePublicationPreservesSourceChoicesAndReportsConflicts(t *testing.T) {
	for _, choice := range []string{"unlinked", "other media", "ambiguous evidence", "gallery disabled"} {
		t.Run(choice, func(t *testing.T) {
			f := newIntakePublicationFixture(t, true)
			repo := f.service.Repo
			if choice == "other media" {
				file := f.publish(t, f.target)
				require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
					owner := models.NewImage()
					return repo.Image.Create(ctx, &models.CreateImageInput{Image: &owner, FileIDs: []models.FileID{file.File.Base().ID}})
				}))
			}
			attachment := f.attachment(t, 0)
			var original *models.AttachmentMediaDecision
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				if choice == "gallery disabled" {
					post, err := repo.SourceEvidence.FindPost(ctx, f.receipt.PostUUID)
					if err != nil {
						return err
					}
					_, err = repo.SourceGallery.DecideAssociation(ctx, models.SourceGalleryChoiceInput{PostUUID: post.UUID, ExpectedPostRevision: post.Revision, State: "disabled", Origin: "review"})
					return err
				}
				input := models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision, State: "unlinked", Origin: "review"}
				if choice != "unlinked" {
					other := models.NewImage()
					if err := repo.Image.Create(ctx, &models.CreateImageInput{Image: &other}); err != nil {
						return err
					}
					identity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveImage, other.ID)
					if err != nil {
						return err
					}
					if choice == "ambiguous evidence" {
						_, err := repo.SourceAttachment.RecordMediaEvidence(ctx, models.SourceMediaEvidence{UUID: uuid.NewString(), AttachmentUUID: attachment.UUID, CaptureUUID: f.receipt.CaptureUUID, MediaUUID: identity.UUID, Basis: "legacy", Details: json.RawMessage(`{}`)})
						return err
					}
					input.State, input.MediaUUID, input.ExpectedMediaRevision = "linked", identity.UUID, identity.Revision
				}
				var err error
				original, err = repo.SourceAttachment.DecideMedia(ctx, input)
				return err
			}))
			result := f.publishIntake(t, f.prepared, f.input)
			switch choice {
			case "gallery disabled":
				require.Equal(t, "disabled", result.Result.Gallery)
				require.Empty(t, result.Result.GalleryUUID)
			case "unlinked":
				require.Equal(t, "unlinked", result.Result.SourceMedia)
			default:
				require.Equal(t, "review", result.Result.SourceMedia)
				require.Contains(t, result.Result.Review, "attachment_media")
			}
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				current, err := repo.SourceAttachment.MediaDecision(ctx, attachment.UUID)
				require.NoError(t, err)
				if choice != "gallery disabled" {
					require.Equal(t, original, current)
				}
				return nil
			}))
		})
	}
}

func TestIntakePublicationUsesSelectedMediaWithoutMovingExistingFiles(t *testing.T) {
	for _, existing := range []string{"no file", "shared file", "different verified owner", "deleted selection", "adopted UUID"} {
		t.Run(existing, func(t *testing.T) {
			f := newIntakePublicationFixture(t, true)
			repo := f.service.Repo
			var selected *models.ArchiveEntity
			var fileIDs []models.FileID
			if existing == "shared file" {
				file := f.publish(t, f.target)
				fileIDs = []models.FileID{file.File.Base().ID}
			}
			if existing == "different verified owner" {
				other, target := f.prepareOther(t, "previous.png")
				require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
					file, err := other.PublishFile(ctx, repo, *target)
					if err != nil {
						return err
					}
					media := models.NewImage()
					return repo.Image.Create(ctx, &models.CreateImageInput{Image: &media, FileIDs: []models.FileID{file.File.Base().ID}})
				}))
			}
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				if existing == "shared file" {
					for range 3 {
						other := models.NewImage()
						if err := repo.Image.Create(ctx, &models.CreateImageInput{Image: &other, FileIDs: fileIDs}); err != nil {
							return err
						}
					}
				}
				media := models.NewImage()
				media.Title = "My selected library item"
				if err := repo.Image.Create(ctx, &models.CreateImageInput{Image: &media, FileIDs: fileIDs}); err != nil {
					return err
				}
				var err error
				selected, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveImage, media.ID)
				if err != nil {
					return err
				}
				attachment, err := repo.SourceAttachment.Find(ctx, f.input.Source.AttachmentUUID)
				if err != nil {
					return err
				}
				if _, err := repo.SourceAttachment.DecideMedia(ctx, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision, State: "linked", MediaUUID: selected.UUID, ExpectedMediaRevision: selected.Revision, Origin: "review"}); err != nil {
					return err
				}
				return nil
			}))
			switch existing {
			case "deleted selection":
				require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Image.Destroy(ctx, *selected.LocalID) }))
			case "adopted UUID":
				require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
					var err error
					selected, err = repo.ArchiveEntity.AdoptUUID(ctx, selected.UUID, uuid.NewString(), selected.Revision)
					return err
				}))
			}
			var result *ingest.PublishedIntake
			err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
				var err error
				result, err = f.prepared.PublishIntake(ctx, repo, f.input)
				return err
			})
			if existing == "deleted selection" {
				require.ErrorIs(t, err, models.ErrSourceAttachmentConflict)
				require.Nil(t, result)
				return
			}
			require.NoError(t, err)
			require.False(t, result.Result.MediaCreated)
			require.Equal(t, selected.UUID, result.Result.MediaUUID)
			require.Equal(t, "linked", result.Result.SourceMedia)
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				media, err := repo.Image.Find(ctx, *selected.LocalID)
				require.NoError(t, err)
				require.Equal(t, "My selected library item", media.Title)
				if existing == "shared file" {
					owners, err := repo.Image.FindByFileID(ctx, fileIDs[0])
					require.NoError(t, err)
					require.Len(t, owners, 4, "selecting an existing owner cannot move or merge the other items")
				}
				return nil
			}))
		})
	}
}

type failingIntakeGallery struct {
	models.SourceGalleryReaderWriter
}

func (f failingIntakeGallery) Sync(context.Context, string, string) (*models.SourceGallerySyncResult, error) {
	return nil, errors.New("fixture gallery publication failure")
}

func TestIntakePublicationRollsBackDomainAndJobWhenGalleryFails(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	repo := f.service.Repo
	repo.SourceGallery = failingIntakeGallery{repo.SourceGallery}
	durable := job.NewDurable(repo)
	args, err := json.Marshal(f.input)
	require.NoError(t, err)
	jobInput := models.ArchiveJobSubmission{RequestUUID: uuid.NewString(), Kind: models.ArchiveJobVerifyMedia, WorkKey: ingest.Digest(args), ResourceKey: ingest.Digest([]byte(f.path)), Arguments: args, MaxAttempts: 3}
	submitted, err := durable.Submit(t.Context(), jobInput)
	require.NoError(t, err)
	claimed, err := durable.Claim(t.Context(), models.ArchiveJobVerifyMedia, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	_, err = durable.Publish(t.Context(), claimed.Lease(), func(ctx context.Context, _ *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
		_, err := f.prepared.PublishIntake(ctx, repo, f.input)
		return models.ArchiveJobOutcome{State: "succeeded", Result: json.RawMessage(`{}`)}, err
	})
	require.ErrorContains(t, err, "fixture gallery publication failure")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.ArchiveJob.Find(ctx, submitted.UUID)
		require.NoError(t, err)
		require.Equal(t, "running", current.State)
		file, err := repo.File.FindByPath(ctx, f.path, true)
		require.NoError(t, err)
		require.Nil(t, file)
		evidence, err := repo.SourceAttachment.MediaEvidence(ctx, f.input.Source.AttachmentUUID, "", 10)
		require.NoError(t, err)
		require.Empty(t, evidence)
		intakes, err := repo.SourceCollection.MediaIntake(ctx, f.collection.UUID, "", 10)
		require.NoError(t, err)
		require.Empty(t, intakes)
		choice, err := repo.SourceAttachment.MediaDecision(ctx, f.input.Source.AttachmentUUID)
		require.NoError(t, err)
		require.Nil(t, choice)
		return nil
	}))
}

func TestIntakePublicationChecksHistoricalAndCurrentCollectionScope(t *testing.T) {
	for _, change := range []string{"label edit", "disabled", "narrowed", "future revision", "old unbound scope", "unrelated capture", "unrelated attachment", "disabled before commit", "unlink before commit"} {
		t.Run(change, func(t *testing.T) {
			f := newIntakePublicationFixture(t, true)
			repo := f.service.Repo
			input := f.input
			if change == "label edit" || change == "disabled" || change == "narrowed" {
				require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
					definition := f.collection.SourceCollectionDefinition
					switch change {
					case "label edit":
						definition.Label = "New display label"
					case "disabled":
						definition.State = "disabled"
					case "narrowed":
						definition.PathPrefix = "different-directory"
					}
					_, err := repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
					return err
				}))
			}
			switch change {
			case "future revision":
				input.CollectionRevision++
			case "old unbound scope":
				input.CollectionRevision--
			case "unrelated capture":
				input.Source = &ingest.IntakeSource{CaptureUUID: uuid.NewString(), AttachmentUUID: input.Source.AttachmentUUID}
			case "unrelated attachment":
				input.Source = &ingest.IntakeSource{CaptureUUID: input.Source.CaptureUUID, AttachmentUUID: uuid.NewString()}
			}
			err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
				if _, err := f.prepared.PublishIntake(ctx, repo, input); err != nil {
					return err
				}
				if change == "disabled before commit" {
					definition := f.collection.SourceCollectionDefinition
					definition.State = "disabled"
					_, err := repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
					return err
				}
				if change == "unlink before commit" {
					attachment, err := repo.SourceAttachment.Find(ctx, input.Source.AttachmentUUID)
					if err != nil {
						return err
					}
					_, err = repo.SourceAttachment.DecideMedia(ctx, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision, State: "unlinked", Origin: "review"})
					return err
				}
				return nil
			})
			if change == "label edit" {
				require.NoError(t, err, "a harmless later label edit must not invalidate accepted historical scope")
			} else {
				require.Error(t, err)
				require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
					file, err := repo.File.FindByPath(ctx, f.path, true)
					require.NoError(t, err)
					require.Nil(t, file)
					intakes, err := repo.SourceCollection.MediaIntake(ctx, f.collection.UUID, "", 10)
					require.NoError(t, err)
					require.Empty(t, intakes)
					return nil
				}))
			}
		})
	}
}
