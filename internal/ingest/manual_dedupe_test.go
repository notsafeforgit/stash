package ingest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestManualDuplicateIntakeDoesNotResurrectRetiredBytesAtNewPath(t *testing.T) {
	for _, retired := range []bool{false, true} {
		t.Run(map[bool]string{false: "reuse_live_owner", true: "preserve_deleted_owner"}[retired], func(t *testing.T) {
			f := newManualFileFixture(t, models.ArchiveImage)
			f.input.DeduplicationPolicy = models.FileDeduplicationEarliest
			worker := f.worker(t, func(ctx context.Context, _ ingest.FileWork, _ ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
				return guard(ctx)
			})
			original := f.request(t)
			_, err := f.service.SubmitManualFile(t.Context(), original)
			require.NoError(t, err)
			processed, err := worker.ProcessNext(t.Context())
			require.NoError(t, err)
			require.True(t, processed)
			first := f.status(t, original.RequestUUID)
			require.Equal(t, "succeeded", first.State)
			body, err := os.ReadFile(f.path)
			require.NoError(t, err)
			copyPath := filepath.Join(filepath.Dir(f.path), "Renamed copy.png")
			require.NoError(t, os.WriteFile(copyPath, body, 0600))
			if retired {
				require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
					owner, err := f.service.Repo.ArchiveEntity.Find(ctx, first.Publication.MediaUUID)
					if err != nil {
						return err
					}
					return f.service.Repo.Image.Destroy(ctx, *owner.LocalID)
				}))
			}
			f.input.RelativePath = filepath.Base(copyPath)
			duplicate := f.request(t)
			_, err = f.service.SubmitManualFile(t.Context(), duplicate)
			require.NoError(t, err)
			processed, err = worker.ProcessNext(t.Context())
			require.NoError(t, err)
			require.True(t, processed)
			second := f.status(t, duplicate.RequestUUID)
			if retired {
				require.Equal(t, "failed", second.State)
				require.False(t, second.RegistrationCommitted)
				require.Nil(t, second.Publication)
			} else {
				require.Equal(t, "succeeded", second.State)
				require.Equal(t, first.Publication.MediaUUID, second.Publication.MediaUUID)
				require.False(t, second.Publication.MediaCreated)
			}
			require.FileExists(t, copyPath)
		})
	}
}
