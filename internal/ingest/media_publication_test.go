package ingest_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/file"
	imagefile "github.com/stashapp/stash/pkg/file/image"
	"github.com/stashapp/stash/pkg/file/video"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func (f publicationFixture) prepareOther(t *testing.T, name string) (*ingest.PreparedMedia, *ingest.FileTarget) {
	t.Helper()
	body, err := os.ReadFile(f.path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(f.root.Binding.Path, name), body, 0600))
	scanner := &file.Scanner{FingerprintCalculator: intakeFingerprinter{}, FileDecorators: []file.Decorator{&imagefile.Decorator{FFProbe: intakeProbe(t)}}}
	prepared, err := ingest.PrepareMedia(t.Context(), *f.root, name, nil, "", scanner)
	require.NoError(t, err)
	t.Cleanup(func() { prepared.Close() })
	return prepared, f.captureTarget(t, name, true)
}

func TestPreparedMediaPublicationRequiresUniqueCurrentByteEvidence(t *testing.T) {
	for _, match := range []string{"verified", "unverified", "stale proof", "ambiguous", "exact owner"} {
		t.Run(match, func(t *testing.T) {
			f := newPublicationFixture(t)
			repo := f.service.Repo
			var existing models.File
			var published *ingest.PublishedFile
			if match == "unverified" {
				existing = f.createScannedFile(t)
			} else {
				published = f.publish(t, f.target)
				existing = published.File
			}
			original := models.NewImage()
			original.Title, original.Organized = "My chosen title", true
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				if err := repo.Image.Create(ctx, &models.CreateImageInput{Image: &original, FileIDs: []models.FileID{existing.Base().ID}}); err != nil {
					return err
				}
				if match == "stale proof" {
					_, err := repo.FileContent.Advance(ctx, published.Identity.UUID, existing.Base().Generation)
					return err
				}
				return nil
			}))
			if match == "ambiguous" || match == "exact owner" {
				other, target := f.prepareOther(t, "other-owner.png")
				require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
					file, err := other.PublishFile(ctx, repo, *target)
					if err != nil {
						return err
					}
					media := models.NewImage()
					return repo.Image.Create(ctx, &models.CreateImageInput{Image: &media, FileIDs: []models.FileID{file.File.Base().ID}})
				}))
			}
			prepared, target := f.prepareOther(t, "incoming.png")
			if match == "exact owner" {
				prepared, target = f.prepared, f.target
			}
			var result *ingest.PublishedMedia
			err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
				var err error
				result, err = prepared.PublishMedia(ctx, repo, *target, models.ArchiveImage)
				return err
			})
			if match == "ambiguous" {
				require.ErrorIs(t, err, ingest.ErrAmbiguousMedia)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				created := match == "unverified" || match == "stale proof"
				require.Equal(t, created, result.Created)
				require.Equal(t, match != "exact owner", result.FileLinked)
				if created {
					require.NotEqual(t, original.ID, *result.Media.LocalID)
				} else {
					require.Equal(t, original.ID, *result.Media.LocalID)
				}
			}
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				retained, err := repo.Image.Find(ctx, original.ID)
				require.NoError(t, err)
				require.Equal(t, original.Title, retained.Title)
				require.True(t, retained.Organized)
				current, err := repo.File.FindByPath(ctx, target.PathFence.Path, true)
				require.NoError(t, err)
				if match == "ambiguous" {
					require.Nil(t, current, "rejected associations must roll back the file and its proof")
				} else {
					require.NotNil(t, current)
				}
				return nil
			}))
		})
	}
}

func TestPreparedMediaPublicationSupportsScenesAndRejectsKindConflicts(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	f := newPublicationFixture(t)
	repo := f.service.Repo
	path := filepath.Join(f.root.Binding.Path, "purchased.mp4")
	output, err := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=32x32:r=5", "-t", "0.4", "-c:v", "mpeg4", "-y", path).CombinedOutput()
	require.NoError(t, err, string(output))
	scanner := &file.Scanner{FingerprintCalculator: intakeFingerprinter{}, FileDecorators: []file.Decorator{&video.Decorator{FFProbe: intakeProbe(t)}}}
	prepared, err := ingest.PrepareMedia(t.Context(), *f.root, "purchased.mp4", nil, "", scanner)
	require.NoError(t, err)
	defer prepared.Close()
	target := f.captureTarget(t, "purchased.mp4", true)
	var result *ingest.PublishedMedia
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = prepared.PublishMedia(ctx, repo, *target, models.ArchiveScene)
		return err
	}))
	require.True(t, result.Created)
	require.Equal(t, models.ArchiveScene, result.Media.Kind)
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := prepared.PublishMedia(ctx, repo, *target, models.ArchiveImage)
		return err
	})
	require.ErrorIs(t, err, ingest.ErrMediaKindConflict)
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.prepared.PublishMedia(ctx, repo, *f.target, models.ArchiveScene)
		return err
	})
	require.ErrorIs(t, err, ingest.ErrUnsupported, "a still image cannot become a scene")
}
