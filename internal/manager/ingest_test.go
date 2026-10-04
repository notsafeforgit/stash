package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeFileWorkerGeneratesRealPreviewsForManualMedia(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe", "avifenc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s required", tool)
		}
	}
	for _, kind := range []models.ArchiveEntityKind{models.ArchiveImage, models.ArchiveScene} {
		t.Run(string(kind), func(t *testing.T) {
			config.InitializeEmpty()
			cfg := config.GetInstance()
			cfg.SetBool(config.CalculateMD5, true)
			db := sqlite.NewDatabase()
			require.NoError(t, db.Open(filepath.Join(t.TempDir(), "manual.sqlite")))
			db.SetBlobStoreOptions(sqlite.BlobStoreOptions{UseDatabase: true})
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			repo := db.Repository()
			generated := paths.NewPaths(t.TempDir(), "")
			mgr := &Manager{Config: cfg, Repository: repo, Database: db, Paths: &generated, ReadLockManager: fsutil.NewReadLockManager(), FFMpeg: ffmpeg.NewEncoder("ffmpeg"), FFProbe: ffmpeg.NewFFProbe("ffprobe")}
			previous := instance
			instance = mgr
			t.Cleanup(func() { instance = previous })
			dir := t.TempDir()
			name := "purchased.png"
			if kind == models.ArchiveScene {
				name = "purchased.mp4"
				out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=64x48:rate=8:duration=1", "-c:v", "libx264", filepath.Join(dir, name)).CombinedOutput()
				require.NoError(t, err, string(out))
			} else {
				var body bytes.Buffer
				require.NoError(t, png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 32, 24))))
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), body.Bytes(), 0600))
			}
			body, err := os.ReadFile(filepath.Join(dir, name))
			require.NoError(t, err)
			binding, err := archive.ProbeMediaRoot(dir)
			require.NoError(t, err)
			var root *models.MediaRoot
			var collection *models.SourceCollection
			var producer *models.IngestProducer
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				var err error
				root, err = repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Purchased files", State: "active", Binding: binding}})
				if err != nil {
					return err
				}
				collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Manual batch", Kind: "manual_batch", State: "active", RootUUID: &root.UUID, PathPrefix: "."}})
				if err != nil {
					return err
				}
				producer, err = repo.Ingest.CreateProducer(ctx, "Manual importer")
				return err
			}))
			service := ingest.New(repo)
			_, token, err := service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID, RootUUID: &root.UUID}}, nil)
			require.NoError(t, err)
			event := ingest.FileEvent{Protocol: 1, ProducerUUID: producer.UUID, EventUUID: uuid.NewString(), RunUUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, RootUUID: root.UUID,
				Kind: "file.completed", ObservedAt: time.Now().UTC(), RelativePath: name, Size: int64(len(body)), SHA256: ingest.Digest(body), MediaKind: kind}
			raw, err := json.Marshal(event)
			require.NoError(t, err)
			_, err = service.FileCompleted(t.Context(), token, raw, ingest.Digest(raw))
			require.NoError(t, err)
			worker := mgr.NewIngestFileWorker(service)
			require.NotNil(t, worker)
			processed, err := worker.ProcessNext(t.Context())
			require.NoError(t, err)
			require.True(t, processed)
			status, err := service.ReceiptStatus(t.Context(), token, event.EventUUID)
			require.NoError(t, err)
			require.Equal(t, "succeeded", status.State, string(status.Result)+" "+status.ErrorCode)
			var result struct {
				Publication ingest.IntakePublicationResult `json:"publication"`
			}
			require.NoError(t, json.Unmarshal(status.Result, &result))
			require.Equal(t, "not_applicable", result.Publication.SourceMedia)
			require.Empty(t, result.Publication.GalleryUUID)
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				entity, err := repo.ArchiveEntity.Resolve(ctx, result.Publication.MediaUUID)
				if err != nil {
					return err
				}
				if kind == models.ArchiveImage {
					img, err := repo.Image.Find(ctx, *entity.LocalID)
					if err != nil {
						return err
					}
					require.NotNil(t, mgr.ImagePreviewImage(img))
				} else {
					scene, err := repo.Scene.Find(ctx, *entity.LocalID)
					if err != nil {
						return err
					}
					require.NotEmpty(t, scene.CoverChecksum)
					source, err := repo.Scene.GetCoverSource(ctx, scene.ID)
					if err != nil {
						return err
					}
					require.NotNil(t, source)
				}
				return nil
			}))
			// Startup capability detection must agree with actual worker support.
			mgr.FFProbe = nil
			require.Nil(t, mgr.NewIngestFileWorker(service))
		})
	}
}
