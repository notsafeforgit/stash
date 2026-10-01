package ingest_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/file"
	imagefile "github.com/stashapp/stash/pkg/file/image"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

type publicationFixture struct {
	captureFixture
	root     *models.MediaRoot
	prepared *ingest.PreparedMedia
	target   *ingest.FileTarget
	path     string
}

func newPublicationFixture(t *testing.T) publicationFixture {
	t.Helper()
	f := publicationFixture{captureFixture: newCaptureFixture(t)}
	definition, path := intakeRoot(t, "image.png", intakePNG(t))
	definition.Label = "Publication fixture"
	f.path = path
	repo := f.service.Repo
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		f.root, err = repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: definition.MediaRootDefinition})
		return err
	}))
	f.target = f.captureTarget(t, "image.png", true)
	scanner := &file.Scanner{FingerprintCalculator: intakeFingerprinter{}, FileDecorators: []file.Decorator{&imagefile.Decorator{FFProbe: intakeProbe(t)}}}
	var err error
	f.prepared, err = ingest.PrepareMedia(t.Context(), *f.root, "image.png", nil, "", scanner)
	require.NoError(t, err)
	t.Cleanup(func() { f.prepared.Close() })
	return f
}

func (f publicationFixture) captureTarget(t *testing.T, relative string, sensitive bool) *ingest.FileTarget {
	t.Helper()
	var target *ingest.FileTarget
	repo := f.service.Repo
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		target, err = ingest.CaptureFileTarget(ctx, repo, *f.root, relative, sensitive)
		return err
	}))
	return target
}

func (f publicationFixture) createScannedFile(t *testing.T) models.File {
	t.Helper()
	repo := f.service.Repo
	media := f.prepared.File()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		folder, err := file.GetOrCreateFolderHierarchy(ctx, repo.Folder, filepath.Dir(f.path), []string{f.root.Binding.Path})
		if err != nil {
			return err
		}
		media.Base().ParentFolderID = folder.ID
		media.Base().CreatedAt, media.Base().UpdatedAt = time.Now(), time.Now()
		return repo.File.Create(ctx, media)
	}))
	return media
}

func (f publicationFixture) publish(t *testing.T, target *ingest.FileTarget) *ingest.PublishedFile {
	t.Helper()
	var ret *ingest.PublishedFile
	repo := f.service.Repo
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = f.prepared.PublishFile(ctx, repo, *target)
		return err
	}))
	return ret
}

func TestPreparedFilePublicationReusesConcurrentScanAndVerifiedReplay(t *testing.T) {
	for _, scanned := range []bool{false, true} {
		t.Run(map[bool]string{false: "new file", true: "concurrent scan"}[scanned], func(t *testing.T) {
			f := newPublicationFixture(t)
			repo := f.service.Repo
			var previous models.File
			if scanned {
				previous = f.createScannedFile(t)
			}
			first := f.publish(t, f.target)
			require.Equal(t, !scanned, first.Created)
			if previous != nil {
				require.Equal(t, previous.Base().ID, first.File.Base().ID)
			}
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				return repo.File.ModifyFingerprints(ctx, first.File.Base().ID, []models.Fingerprint{{Type: models.FingerprintTypePhash, Fingerprint: int64(1234)}})
			}))
			second := f.publish(t, f.target)
			require.False(t, second.Created)
			require.Equal(t, first.Proof, second.Proof)
			require.Equal(t, int64(1234), second.File.Base().Fingerprints.Get(models.FingerprintTypePhash))
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				locations, err := repo.FileContent.Locations(ctx, first.Proof.Content.UUID, "", 10)
				require.NoError(t, err)
				require.Len(t, locations, 1)
				images, err := repo.Image.FindByFileID(ctx, first.File.Base().ID)
				require.NoError(t, err)
				require.Empty(t, images, "file publication cannot silently run legacy fingerprint matching handlers")
				return nil
			}))
		})
	}
}

func TestPreparedFilePublicationRejectsRemovedPathsAndStaleGenerations(t *testing.T) {
	for _, change := range []string{"create then delete", "known file deleted", "renamed", "generation changed"} {
		t.Run(change, func(t *testing.T) {
			f := newPublicationFixture(t)
			repo := f.service.Repo
			existing := f.createScannedFile(t)
			target := f.target
			if change != "create then delete" {
				target = f.captureTarget(t, "image.png", true)
			}
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				switch change {
				case "create then delete", "known file deleted":
					return repo.File.Destroy(ctx, existing.Base().ID)
				case "renamed":
					existing.Base().Basename = "renamed.png"
				case "generation changed":
					existing.Base().Size++
				}
				return repo.File.Update(ctx, existing)
			}))
			err := repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := f.prepared.PublishFile(ctx, repo, *target); return err })
			if change == "generation changed" {
				require.ErrorIs(t, err, models.ErrFileGenerationConflict)
			} else {
				require.ErrorIs(t, err, models.ErrFilePathChanged)
			}
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				content, err := repo.FileContent.FindBySHA256(ctx, f.prepared.SHA256())
				require.NoError(t, err)
				require.Nil(t, content)
				return nil
			}))
		})
	}
}

func TestPreparedFilePublicationRequiresNewFileLifetimeAfterRemoval(t *testing.T) {
	f := newPublicationFixture(t)
	repo := f.service.Repo
	first := f.publish(t, f.target)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.File.Destroy(ctx, first.File.Base().ID) }))
	target := f.captureTarget(t, "image.png", true)
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := f.prepared.PublishFile(ctx, repo, *target); return err })
	require.ErrorIs(t, err, models.ErrFilePathChanged, "a newly delivered completion cannot resurrect an absent removed file")
	replacement := f.createScannedFile(t)
	newTarget := f.captureTarget(t, "image.png", true)
	restored := f.publish(t, newTarget)
	require.False(t, restored.Created)
	require.Equal(t, replacement.Base().ID, restored.File.Base().ID)
	require.NotEqual(t, first.Identity.UUID, restored.Identity.UUID)
	// Canonical adoption changes UUID spelling, not this file's lifetime.
	before := restored.Identity
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, before.UUID, uuid.NewString(), before.Revision)
		return err
	}))
	adopted := f.publish(t, newTarget)
	require.NotEqual(t, restored.Identity.UUID, adopted.Identity.UUID)
	require.Equal(t, restored.File.Base().ID, adopted.File.Base().ID)
}

func TestPreparedFilePublicationValidatesCaseLookupAliasesByDescriptor(t *testing.T) {
	for _, hardlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "distinct files", true: "same file alias"}[hardlink], func(t *testing.T) {
			f := newPublicationFixture(t)
			repo := f.service.Repo
			f.createScannedFile(t)
			upper := filepath.Join(filepath.Dir(f.path), "IMAGE.PNG")
			if hardlink {
				require.NoError(t, os.Link(f.path, upper))
			} else {
				require.NoError(t, os.WriteFile(upper, intakePNG(t), 0600))
			}
			scanner := &file.Scanner{FingerprintCalculator: intakeFingerprinter{}, FileDecorators: []file.Decorator{&imagefile.Decorator{FFProbe: intakeProbe(t)}}}
			prepared, err := ingest.PrepareMedia(t.Context(), *f.root, "IMAGE.PNG", nil, "", scanner)
			require.NoError(t, err)
			defer prepared.Close()
			target := f.captureTarget(t, "IMAGE.PNG", false)
			require.NotEmpty(t, target.FileUUID)
			err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := prepared.PublishFile(ctx, repo, *target)
				return err
			})
			if hardlink {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, archive.ErrMediaFileChanged, "equal bytes and case-folded text do not establish the same file identity")
			}
		})
	}
}

func TestPreparedFilePublicationAndJobCompletionCommitTogether(t *testing.T) {
	for _, change := range []string{"success", "file changed", "lease expired", "file deleted", "media deleted", "media detached", "extra owner"} {
		t.Run(change, func(t *testing.T) {
			f := newPublicationFixture(t)
			repo := f.service.Repo
			durable := job.NewDurable(repo)
			now := time.Now()
			durable.Now = func() time.Time { return now }
			args, err := json.Marshal(f.target)
			require.NoError(t, err)
			pending, err := durable.Submit(t.Context(), models.ArchiveJobSubmission{RequestUUID: uuid.NewString(), Kind: models.ArchiveJobVerifyMedia,
				WorkKey: ingest.Digest(args), ResourceKey: ingest.Digest([]byte(f.path)), Arguments: args, MaxAttempts: 3})
			require.NoError(t, err)
			claimed, err := durable.Claim(t.Context(), models.ArchiveJobVerifyMedia, uuid.NewString(), time.Minute)
			require.NoError(t, err)
			require.Equal(t, pending.UUID, claimed.UUID)
			result, err := durable.Publish(t.Context(), claimed.Lease(), func(ctx context.Context, _ *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
				published, err := f.prepared.PublishMedia(ctx, repo, *f.target, models.ArchiveImage)
				if err != nil {
					return models.ArchiveJobOutcome{}, err
				}
				switch change {
				case "file changed":
					require.NoError(t, os.WriteFile(f.path, []byte("changed before commit"), 0600))
				case "lease expired":
					txn.AddPreCommitHook(ctx, func(context.Context) error { now = *claimed.LeaseUntil; return nil })
				case "file deleted":
					if err := repo.File.Destroy(ctx, published.File.File.Base().ID); err != nil {
						return models.ArchiveJobOutcome{}, err
					}
				case "media deleted":
					if err := repo.Image.Destroy(ctx, *published.Media.LocalID); err != nil {
						return models.ArchiveJobOutcome{}, err
					}
				case "media detached":
					if err := repo.Image.RemoveFileID(ctx, *published.Media.LocalID, published.File.File.Base().ID); err != nil {
						return models.ArchiveJobOutcome{}, err
					}
				case "extra owner":
					other := models.NewImage()
					if err := repo.Image.Create(ctx, &models.CreateImageInput{Image: &other, FileIDs: []models.FileID{published.File.File.Base().ID}}); err != nil {
						return models.ArchiveJobOutcome{}, err
					}
				}
				return models.ArchiveJobOutcome{State: "succeeded", Result: json.RawMessage(`{"media_ingested":true}`)}, nil
			})
			if change == "success" {
				require.NoError(t, err)
				require.Equal(t, "succeeded", result.State)
			} else {
				require.Error(t, err)
				require.Nil(t, result)
			}
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				stored, err := repo.ArchiveJob.Find(ctx, pending.UUID)
				require.NoError(t, err)
				file, err := repo.File.FindByPath(ctx, f.path, true)
				require.NoError(t, err)
				content, err := repo.FileContent.FindBySHA256(ctx, f.prepared.SHA256())
				require.NoError(t, err)
				if change == "success" {
					require.Equal(t, "succeeded", stored.State)
					require.NotNil(t, file)
					require.NotNil(t, content)
					images, err := repo.Image.FindByFileID(ctx, file.Base().ID)
					require.NoError(t, err)
					require.Len(t, images, 1)
				} else {
					require.Equal(t, "running", stored.State)
					require.Nil(t, file)
					require.Nil(t, content)
				}
				return nil
			}))
		})
	}
}
