package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestDeduplicatedRedownloadEvidenceForScenesAndImages(t *testing.T) {
	for _, image := range []bool{false, true} {
		t.Run(map[bool]string{false: "scene", true: "image"}[image], func(t *testing.T) {
			f := newDeduplicationFixture(t, image)
			receipt, err := f.service.Apply(t.Context(), f.request(t))
			require.NoError(t, err)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				fence, err := f.repo.FilePath.Snapshot(ctx, f.duplicate.Base().Path, true)
				require.NoError(t, err)
				require.EqualValues(t, 1, fence.Revision)
				owner, err := f.repo.FileDeduplication.RedownloadOwner(ctx, models.FileRedownload{
					RootUUID: f.root.UUID, RelativePath: f.input.RemovePath, Fence: *fence, SHA256: receipt.SHA256, Size: f.file.Base().Size,
				})
				require.NoError(t, err)
				require.Equal(t, receipt.MediaUUID, owner.UUID)
				require.Equal(t, map[bool]models.ArchiveEntityKind{false: models.ArchiveScene, true: models.ArchiveImage}[image], owner.Kind)
				return nil
			}))
		})
	}
}
