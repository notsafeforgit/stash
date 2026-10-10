package ingest_test

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/gif"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/file"
	imagefile "github.com/stashapp/stash/pkg/file/image"
	"github.com/stashapp/stash/pkg/file/video"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

type imageConversionFixture struct {
	intakePublicationFixture
	before   *ingest.PublishedIntake
	event    ingest.FileEvent
	prepared *ingest.PreparedMedia
	input    ingest.IntakePublication
}

func newImageConversionFixture(t *testing.T) imageConversionFixture {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	f := newIntakePublicationFixture(t, true)
	name := filepath.Join(f.root.Binding.Path, "animation.gif")
	output, err := os.Create(name)
	require.NoError(t, err)
	require.NoError(t, gif.Encode(output, image.NewRGBA(image.Rect(0, 0, 32, 32)), nil))
	require.NoError(t, output.Close())
	scanner := &file.Scanner{FingerprintCalculator: intakeFingerprinter{}, FileDecorators: []file.Decorator{&imagefile.Decorator{FFProbe: intakeProbe(t)}}}
	original, err := ingest.PrepareMedia(t.Context(), *f.root, "animation.gif", nil, "", scanner)
	require.NoError(t, err)
	input := f.input
	input.Target = *f.captureTarget(t, "animation.gif", true)
	before := f.publishIntake(t, original, input)
	require.NoError(t, original.Close())
	event := convertedFileEvent(t, f)
	final := filepath.Join(f.root.Binding.Path, event.RelativePath)
	log, err := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=32x32:r=5", "-t", "0.4", "-c:v", "ffv1", "-y", final).CombinedOutput()
	require.NoError(t, err, string(log))
	scanner.FileDecorators = []file.Decorator{&video.Decorator{FFProbe: intakeProbe(t)}}
	prepared, err := ingest.PrepareMedia(t.Context(), *f.root, event.RelativePath, nil, "", scanner)
	require.NoError(t, err)
	t.Cleanup(func() { _ = prepared.Close() })
	event.Size, event.SHA256 = prepared.Snapshot().Size, prepared.SHA256()
	input.UUID, input.Kind = uuid.NewString(), models.ArchiveScene
	input.Target = *f.captureTarget(t, event.RelativePath, true)
	input.Transformation = &ingest.IntakeTransformation{Kind: "gif-to-video", Original: *f.captureTarget(t, "animation.gif", true)}
	require.NoError(t, os.Remove(name))
	return imageConversionFixture{f, before, event, prepared, input}
}

func TestImageConversionRetainsChoicesIdentityAlbumsAndUndatedCount(t *testing.T) {
	f := newImageConversionFixture(t)
	repo := f.service.Repo
	id := *f.before.Media.Media.LocalID
	manual := models.NewGallery()
	manual.Title = "Purchased and scraped favorites"
	performer := models.NewPerformer()
	performer.Name = "Chosen performer"
	tag := models.NewTag()
	tag.Name = "Retained tag"
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		require.NoError(t, repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &performer}))
		require.NoError(t, repo.Tag.Create(ctx, &models.CreateTagInput{Tag: &tag}))
		require.NoError(t, repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &manual}))
		require.NoError(t, repo.Gallery.AddImages(ctx, manual.ID, id))
		require.NoError(t, repo.Gallery.SetCover(ctx, manual.ID, f.before.Result.MediaUUID))
		album, err := repo.ArchiveEntity.Find(ctx, f.before.Result.GalleryUUID)
		require.NoError(t, err)
		require.NoError(t, repo.Gallery.SetCover(ctx, *album.LocalID, f.before.Result.MediaUUID))
		patch := models.NewImagePartial()
		patch.Title = models.NewOptionalString("My chosen title")
		patch.Photographer = models.NewOptionalString("Photographer credit")
		patch.Organized = models.NewOptionalBool(true)
		patch.OCounter = models.NewOptionalInt(3)
		patch.URLs = &models.UpdateStrings{Mode: models.RelationshipUpdateModeSet, Values: []string{"https://example.test/original"}}
		patch.PerformerIDs = &models.UpdateIDs{Mode: models.RelationshipUpdateModeSet, IDs: []int{performer.ID}}
		patch.TagIDs = &models.UpdateIDs{Mode: models.RelationshipUpdateModeSet, IDs: []int{tag.ID}}
		patch.CustomFields = models.CustomFieldsInput{Full: map[string]interface{}{"source_note": "Purchased file"}}
		_, err = repo.Image.UpdatePartial(ctx, id, patch)
		require.NoError(t, err)
		entity, err := repo.ArchiveEntity.Find(ctx, f.before.Result.MediaUUID)
		require.NoError(t, err)
		_, err = repo.MetadataField.Decide(ctx, models.MetadataFieldDecisionInput{
			EntityUUID: entity.UUID, ExpectedEntityRevision: entity.Revision, Field: "details", Mode: "clear", Origin: "review", Reason: "Keep my description empty",
		})
		return err
	}))
	result := f.publishIntake(t, f.prepared, f.input)
	require.NotNil(t, result.Result.Conversion)
	require.Equal(t, f.before.Result.GalleryUUID, result.Result.GalleryUUID)
	require.Equal(t, "linked", result.Result.SourceMedia)
	require.Empty(t, result.Result.Review)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		old, err := repo.ArchiveEntity.Find(ctx, f.before.Result.MediaUUID)
		require.NoError(t, err)
		require.Equal(t, models.ArchiveImage, old.Kind)
		require.Equal(t, models.ArchiveEntityRedirected, old.State)
		resolved, err := repo.ArchiveEntity.Resolve(ctx, old.UUID)
		require.NoError(t, err)
		require.Equal(t, result.Result.MediaUUID, resolved.UUID)
		scene, err := repo.Scene.Find(ctx, *resolved.LocalID)
		require.NoError(t, err)
		require.Equal(t, "My chosen title", scene.Title)
		require.True(t, scene.Organized)
		require.Empty(t, scene.Director, "photographer is not a director")
		performers, err := repo.Scene.GetPerformerIDs(ctx, scene.ID)
		require.NoError(t, err)
		require.Equal(t, []int{performer.ID}, performers)
		tags, err := repo.Scene.GetTagIDs(ctx, scene.ID)
		require.NoError(t, err)
		require.Equal(t, []int{tag.ID}, tags)
		custom, err := repo.Scene.GetCustomFields(ctx, scene.ID)
		require.NoError(t, err)
		require.Equal(t, map[string]interface{}{"source_note": "Purchased file"}, custom)
		count, err := repo.Scene.GetOCount(ctx, scene.ID)
		require.NoError(t, err)
		require.Equal(t, 3, count)
		counts, err := repo.Scene.GetManyOCount(ctx, []int{scene.ID})
		require.NoError(t, err)
		require.Equal(t, []int{3}, counts)
		dates, err := repo.Scene.GetODates(ctx, scene.ID)
		require.NoError(t, err)
		require.Empty(t, dates, "an image counter cannot supply timestamps")
		conversion, err := repo.MediaConversion.Find(ctx, result.Result.Conversion.UUID)
		require.NoError(t, err)
		require.Equal(t, "Photographer credit", conversion.Photographer)
		oldFile, err := repo.ArchiveEntity.Find(ctx, f.input.Transformation.Original.FileUUID)
		require.NoError(t, err)
		require.Equal(t, models.ArchiveEntityDeleted, oldFile.State)
		state, err := repo.MetadataField.State(ctx, sceneUUID(result), "details")
		require.NoError(t, err)
		require.Equal(t, "clear", state.Mode)
		require.True(t, state.Protected)
		require.Equal(t, "Keep my description empty", state.Decision.Reason)
		history, err := repo.MetadataField.History(ctx, old.UUID, "details", 0, 10)
		require.NoError(t, err)
		require.NotEmpty(t, history)
		for _, galleryUUID := range result.Result.Conversion.GalleryUUIDs {
			g, err := repo.ArchiveEntity.Find(ctx, galleryUUID)
			require.NoError(t, err)
			gallery, err := repo.Gallery.Find(ctx, *g.LocalID)
			require.NoError(t, err)
			require.Equal(t, sceneUUID(result), *gallery.CoverMediaUUID)
			images, err := repo.Gallery.GetImageIDs(ctx, gallery.ID)
			require.NoError(t, err)
			require.Empty(t, images)
			scenes, err := repo.Gallery.GetSceneIDs(ctx, gallery.ID)
			require.NoError(t, err)
			require.Equal(t, []int{scene.ID}, scenes)
		}
		return nil
	}))
	name := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(name), "converted identities and metadata must pass normal startup validation")
}

func sceneUUID(result *ingest.PublishedIntake) string { return result.Result.MediaUUID }

func TestImageConversionRetainsReviewedPostAndPolicyProvenance(t *testing.T) {
	f := newImageConversionFixture(t)
	repo := f.service.Repo
	policy := f.policy(t, 0, map[string]models.MetadataMapping{"title": {JQ: `.source.metadata.title`}})
	f.input.PolicyRevision = policy.Revision
	var decision *models.MetadataFieldState
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		association, err := repo.SourcePostMedia.Association(ctx, f.receipt.PostUUID, f.before.Result.MediaUUID)
		require.NoError(t, err)
		choice, err := repo.SourcePostMedia.Decide(ctx, models.SourcePostMediaInput{
			UUID: uuid.NewString(), PostUUID: f.receipt.PostUUID, MediaUUID: f.before.Result.MediaUUID,
			ExpectedPostRevision: association.PostRevision, ExpectedMediaRevision: association.MediaRevision,
			ExpectedDecisions: []string{}, State: "linked", Origin: "review",
		})
		require.NoError(t, err)
		entity, err := repo.ArchiveEntity.Find(ctx, f.before.Result.MediaUUID)
		require.NoError(t, err)
		decision, err = repo.MetadataField.ApplyAutomatic(ctx, models.MetadataFieldDecisionInput{
			EntityUUID: entity.UUID, ExpectedEntityRevision: entity.Revision, Field: "title", Mode: "inherit",
			Value: json.RawMessage(`"Selected post caption"`), Origin: "source", CaptureUUID: f.receipt.CaptureUUID,
			PostMediaDecisionUUID: choice.UUID, Policy: &policy.MetadataPolicyRef,
		})
		return err
	}))
	result := f.publishIntake(t, f.prepared, f.input)
	state := intakeField(t, repo, result.Result.MediaUUID, "title")
	require.JSONEq(t, string(decision.Value), string(state.Value))
	require.Equal(t, "inherit", state.Mode)
	require.False(t, state.Protected)
	require.Equal(t, "source", state.Origin)
	require.Equal(t, decision.Decision.CaptureUUID, state.Decision.CaptureUUID)
	require.Equal(t, decision.Decision.PostMediaDecisionUUID, state.Decision.PostMediaDecisionUUID)
	require.Equal(t, decision.Decision.Policy, state.Decision.Policy)
	require.NotEqual(t, decision.Decision.UUID, state.Decision.UUID)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		history, err := repo.MetadataField.History(ctx, f.before.Result.MediaUUID, "title", 0, 10)
		require.NoError(t, err)
		require.NotEmpty(t, history)
		require.Equal(t, decision.Decision.UUID, history[0].UUID)
		return nil
	}))
	name := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(name), "copied source provenance must pass normal startup validation")
}

func TestImageConversionRollsBackWhenOriginalOrOwnershipChanges(t *testing.T) {
	for _, change := range []string{"present", "symlink", "generation", "different owner", "shared file", "second file", "existing scene"} {
		t.Run(change, func(t *testing.T) {
			f := newImageConversionFixture(t)
			repo := f.service.Repo
			original := f.input.Transformation.Original
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				switch change {
				case "present":
					return os.WriteFile(original.PathFence.Path, []byte("another download"), 0600)
				case "symlink":
					return os.Symlink("does-not-exist.gif", original.PathFence.Path)
				case "generation":
					_, err := repo.FileContent.Advance(ctx, original.FileUUID, original.Generation)
					return err
				case "different owner", "shared file":
					other := models.NewImage()
					require.NoError(t, repo.Image.Create(ctx, &models.CreateImageInput{Image: &other, FileIDs: []models.FileID{f.before.Media.File.File.Base().ID}}))
					if change == "different owner" {
						return repo.Image.RemoveFileID(ctx, *f.before.Media.Media.LocalID, f.before.Media.File.File.Base().ID)
					}
				case "second file":
					published, err := f.publicationFixture.prepared.PublishFile(ctx, repo, *f.target)
					if err != nil {
						return err
					}
					return repo.Image.AddFileID(ctx, *f.before.Media.Media.LocalID, published.File.Base().ID)
				case "existing scene":
					_, err := f.prepared.PublishMedia(ctx, repo, f.input.Target, models.ArchiveScene)
					return err
				}
				return nil
			}))
			err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.prepared.PublishIntake(ctx, repo, f.input)
				return err
			})
			require.Error(t, err)
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				old, err := repo.ArchiveEntity.Find(ctx, f.before.Result.MediaUUID)
				require.NoError(t, err)
				require.Equal(t, models.ArchiveEntityActive, old.State)
				file, err := repo.ArchiveEntity.Find(ctx, original.FileUUID)
				require.NoError(t, err)
				require.Equal(t, models.ArchiveEntityActive, file.State)
				return nil
			}))
		})
	}
}

func TestImageConversionWorkerResumesAfterCommittedConversion(t *testing.T) {
	f := newImageConversionFixture(t)
	_, err := f.submitFile(t, f.event)
	require.NoError(t, err)
	scanner := &file.Scanner{FingerprintCalculator: intakeFingerprinter{}, FileDecorators: []file.Decorator{&video.Decorator{FFProbe: intakeProbe(t)}}}
	worker := ingest.NewFileWorker(f.service, func(models.ArchiveEntityKind) *file.Scanner { return scanner },
		func(context.Context, ingest.FileWork, ingest.IntakePublicationResult, ingest.FileEffectGuard) error {
			return errors.New("simulated preview failure")
		})
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	_, pending := f.fileStatus(t, f.event.EventUUID)
	require.True(t, pending.RegistrationCommitted)
	require.NotNil(t, pending.Publication.Conversion)
	name := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(name))
	f.service = ingest.New(f.db.Repository())
	worker = ingest.NewFileWorker(f.service, func(models.ArchiveEntityKind) *file.Scanner { return scanner },
		func(ctx context.Context, _ ingest.FileWork, result ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
			require.Equal(t, pending.Publication, &result)
			return guard(ctx)
		})
	worker.Durable.Now = func() time.Time { return time.Now().Add(time.Minute) }
	processed, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	status, result := f.fileStatus(t, f.event.EventUUID)
	require.Equal(t, "succeeded", status.State)
	require.True(t, result.MediaIngested)
	before, err := json.Marshal(pending.Publication)
	require.NoError(t, err)
	after, err := json.Marshal(result.Publication)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
}

func TestImageConversionPreservesExplicitSourceAndGalleryUnlinks(t *testing.T) {
	for _, choice := range []string{"attachment", "post", "gallery"} {
		t.Run(choice, func(t *testing.T) {
			f := newImageConversionFixture(t)
			repo := f.service.Repo
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				if choice == "gallery" {
					gallery, err := repo.ArchiveEntity.Find(ctx, f.before.Result.GalleryUUID)
					if err != nil {
						return err
					}
					return repo.Gallery.RemoveImages(ctx, *gallery.LocalID, *f.before.Media.Media.LocalID)
				}
				attachment, err := repo.SourceAttachment.Find(ctx, f.input.Source.AttachmentUUID)
				require.NoError(t, err)
				if choice == "attachment" {
					_, err := repo.SourceAttachment.DecideMedia(ctx, models.AttachmentMediaDecisionInput{
						AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision,
						State: "unlinked", Origin: "review",
					})
					return err
				}
				association, err := repo.SourcePostMedia.Association(ctx, attachment.PostUUID, f.before.Result.MediaUUID)
				require.NoError(t, err)
				_, err = repo.SourcePostMedia.Decide(ctx, models.SourcePostMediaInput{
					UUID: uuid.NewString(), PostUUID: attachment.PostUUID, MediaUUID: f.before.Result.MediaUUID,
					ExpectedPostRevision: association.PostRevision, ExpectedMediaRevision: association.MediaRevision,
					ExpectedDecisions: []string{}, State: "unlinked", Origin: "review",
				})
				return err
			}))
			result := f.publishIntake(t, f.prepared, f.input)
			require.NotNil(t, result.Result.Conversion)
			if choice != "gallery" {
				require.Equal(t, "unlinked", result.Result.SourceMedia)
			}
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				gallery, err := repo.ArchiveEntity.Find(ctx, f.before.Result.GalleryUUID)
				require.NoError(t, err)
				members, err := repo.Gallery.GetSceneIDs(ctx, *gallery.LocalID)
				require.NoError(t, err)
				require.Empty(t, members, "conversion must not override the explicit unlink")
				return nil
			}))
		})
	}
}

func TestImageConversionFollowsUUIDAdoptionAndPreservesCountersAfterChanges(t *testing.T) {
	f := newImageConversionFixture(t)
	repo := f.service.Repo
	result := f.publishIntake(t, f.prepared, f.input)
	var id int
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		entity, err := repo.ArchiveEntity.Find(ctx, result.Result.MediaUUID)
		require.NoError(t, err)
		entity, err = repo.ArchiveEntity.AdoptUUID(ctx, entity.UUID, uuid.NewString(), entity.Revision)
		require.NoError(t, err)
		id = *entity.LocalID
		require.NoError(t, repo.Scene.AddUndatedO(ctx, id, 3))
		_, err = repo.Scene.AddO(ctx, id, []time.Time{time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)})
		return err
	}))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		count, err := repo.Scene.GetOCount(ctx, id)
		require.NoError(t, err)
		require.Equal(t, 4, count)
		for expected := 3; expected >= 0; expected-- {
			_, err := repo.Scene.DeleteO(ctx, id, nil)
			require.NoError(t, err)
			count, err := repo.Scene.GetOCount(ctx, id)
			require.NoError(t, err)
			require.Equal(t, expected, count)
		}
		return nil
	}))
	name := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(name))
}
