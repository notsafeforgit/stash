package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/file"
	imagepkg "github.com/stashapp/stash/pkg/image"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stashapp/stash/pkg/plugin"
	"github.com/stashapp/stash/pkg/scene"
	"github.com/stretchr/testify/require"
)

type policySceneGenerator struct{}

func (policySceneGenerator) Generate(context.Context, *models.Scene, *models.VideoFile) error {
	return nil
}

type policyImageGenerator struct{}

func (policyImageGenerator) Generate(context.Context, *models.Image, models.File) error { return nil }

type policyScanConfig struct{}

func (policyScanConfig) GetCreateGalleriesFromFolders() bool { return false }

func TestMetadataPolicyOrdinaryScanHandlersApplyWithoutSourceAndRescanSafely(t *testing.T) {
	for _, kind := range []models.ArchiveEntityKind{models.ArchiveScene, models.ArchiveImage} {
		t.Run(string(kind), func(t *testing.T) {
			f := newMetadataPolicyFixture(t)
			name := "Purchased file.mp4"
			if kind == models.ArchiveImage {
				name = "Purchased file.jpg"
			}
			filePath := filepath.Join(f.root.Binding.Path, name)
			require.NoError(t, os.WriteFile(filePath, []byte("already scanned file fixture"), 0600))
			rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, FilenameTitleFallback: true}
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.MetadataPolicy.Put(ctx, models.MetadataPolicyInput{CollectionUUID: f.collection.UUID, ExpectedCollectionRevision: f.collection.Revision, Origin: "review", Definition: models.MetadataPolicyDefinition{Enabled: true, ApplyToScans: true, Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{kind: rule}}})
				if err != nil {
					return err
				}
				_, _, err = f.db.ExecSQL(ctx, `UPDATE folders SET path=? WHERE id=1`, []interface{}{f.root.Binding.Path})
				if err != nil {
					return err
				}
				_, _, err = f.db.ExecSQL(ctx, `UPDATE files SET basename=? WHERE id=21`, []interface{}{name})
				if err != nil {
					return err
				}
				if kind == models.ArchiveScene {
					_, _, err = f.db.ExecSQL(ctx, `INSERT INTO video_files(file_id,duration,video_codec,format,audio_codec,width,height,frame_rate,bit_rate) VALUES(21,1,'h264','mp4','aac',64,48,30,100)`, nil)
				}
				return err
			}))
			var preview *metadata.Preview
			apply := func(ctx context.Context, kind models.ArchiveEntityKind, id int, media models.File, created bool) error {
				var err error
				preview, err = (metadata.Service{Repo: f.repo}).ApplyScan(ctx, kind, id, media, created)
				return err
			}
			generated := paths.NewPaths(t.TempDir(), "")
			cache := plugin.NewCache(config.GetInstance())
			var handler file.Handler
			if kind == models.ArchiveScene {
				handler = &scene.ScanHandler{CreatorUpdater: f.repo.Scene, GalleryFinderUpdater: f.repo.Gallery, CaptionUpdater: f.repo.File, ScanGenerator: policySceneGenerator{}, PluginCache: cache, MetadataPolicy: apply, FileNamingAlgorithm: models.HashAlgorithmMd5, Paths: &generated}
			} else {
				handler = &imagepkg.ScanHandler{CreatorUpdater: f.repo.Image, GalleryFinder: f.repo.Gallery, SceneFinderUpdater: f.repo.Scene, ScanGenerator: policyImageGenerator{}, ScanConfig: policyScanConfig{}, PluginCache: cache, MetadataPolicy: apply, Paths: &generated}
			}
			scan := func() {
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					files, err := f.repo.File.Find(ctx, 21)
					if err != nil {
						return err
					}
					return handler.Handle(ctx, files[0], nil)
				}))
			}
			scan()
			require.NotNil(t, preview)
			require.True(t, preview.Input.Created)
			entity := preview.Input.EntityUUID
			require.Equal(t, `"Purchased file"`, string(metadataState(t, f.repo, entity, "title").Value))
			require.Equal(t, "[]", string(metadataState(t, f.repo, entity, "performers").Value))
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				intakes, err := f.repo.SourceCollection.MediaIntake(ctx, f.collection.UUID, "", 100)
				require.Len(t, intakes, 1)
				require.Equal(t, "scan", intakes[0].Origin)
				captures, err2 := f.repo.SourceCollection.Captures(ctx, f.collection.UUID, nil, 100)
				require.NoError(t, err2)
				require.Empty(t, captures)
				return err
			}))
			_, err := applyMetadata(f.repo, metadataInput(metadataState(t, f.repo, entity, "title"), "clear", "review", ""), false)
			require.NoError(t, err)
			scan()
			require.False(t, preview.Input.Created)
			require.Empty(t, preview.AppliedFields())
			require.Equal(t, `""`, string(metadataState(t, f.repo, entity, "title").Value))
		})
	}
}
