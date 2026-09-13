package manager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/hash/md5"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stashapp/stash/pkg/previewimage"
	"github.com/stretchr/testify/mock"
)

func coverTestManager(t *testing.T) (*Manager, *mocks.Database) {
	t.Helper()
	config.InitializeEmpty()
	cfg := config.GetInstance()
	cfg.SetBool(config.EnableV3UI, true)
	db := mocks.NewDatabase()
	jobs := job.NewManager()
	jobs.SetSync(true)
	t.Cleanup(jobs.Stop)
	testPaths := paths.NewPaths(t.TempDir(), "")
	mgr := &Manager{
		Config: cfg, Repository: db.Repository(), JobManager: jobs,
		Paths: &testPaths, ReadLockManager: fsutil.NewReadLockManager(),
	}
	return mgr, db
}

func saveCoverTestMetadata(t *testing.T, mgr *Manager, scene *models.Scene, at float64) {
	t.Helper()
	key, err := previewimage.SourceKey(scene.Path, scene.CoverChecksum)
	if err != nil {
		t.Fatal(err)
	}
	result := &previewimage.Result{
		Directory: t.TempDir(),
		Variants:  []previewimage.Variant{{File: "preview.jpg", MIMEType: "image/jpeg", DynamicRange: previewimage.SDR, Width: 64, Height: 48}},
	}
	if err := os.WriteFile(filepath.Join(result.Directory, "preview.jpg"), []byte("cover"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := mgr.PreviewImageStore().Publish(scene.ID, "cover", key, at, result); err != nil {
		t.Fatal(err)
	}
}

func TestScenePreviewImageRequiresCurrentArtwork(t *testing.T) {
	mgr, _ := coverTestManager(t)
	scene := models.Scene{ID: 1, Path: filepath.Join(t.TempDir(), "video.mkv"), CoverChecksum: "cover"}
	if err := os.WriteFile(scene.Path, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	if mgr.ScenePreviewImage(&scene) != nil {
		t.Fatal("advertised renditions for a legacy cover")
	}
	saveCoverTestMetadata(t, mgr, &scene, 0)
	if manifest := mgr.ScenePreviewImage(&scene); manifest == nil || manifest.At != 0 {
		t.Fatalf("missing generated cover: %v", manifest)
	}
	scene.CoverChecksum = "uploaded cover"
	if mgr.ScenePreviewImage(&scene) != nil {
		t.Fatal("uploaded artwork inherited generated renditions")
	}
	scene.CoverChecksum = "cover"
	mgr.Config.SetBool(config.EnableV3UI, false)
	if mgr.ScenePreviewImage(&scene) != nil {
		t.Fatal("exposed v3 previews with v3 disabled")
	}
	mgr.Config.SetBool(config.EnableV3UI, true)
	if err := os.WriteFile(scene.Path, []byte("replacement source"), 0600); err != nil {
		t.Fatal(err)
	}
	if mgr.ScenePreviewImage(&scene) != nil {
		t.Fatal("replacement source inherited old renditions")
	}
}

func TestGenerateScreenshotPublishesPreviewAndReportsWriteFailure(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required for cover regeneration")
	}
	probePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is required for cover regeneration")
	}
	for _, at := range []float64{0, 1.125} {
		t.Run(fmt.Sprintf("at_%g", at), func(t *testing.T) {
			mgr, db := coverTestManager(t)
			mgr.FFMpeg, mgr.FFProbe = ffmpeg.NewEncoder(ffmpegPath), ffmpeg.NewFFProbe(probePath)
			previous := instance
			instance = mgr
			t.Cleanup(func() { instance = previous })
			source := filepath.Join(t.TempDir(), "source.mkv")
			out, err := exec.Command(ffmpegPath, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=64x48:rate=8:duration=3", "-c:v", "ffv1", source).CombinedOutput()
			if err != nil {
				t.Fatalf("fixture: %v: %s", err, out)
			}
			file := &models.VideoFile{BaseFile: &models.BaseFile{Path: source}, Width: 64, Height: 48, Duration: 3}
			scene := models.Scene{ID: 1, Path: source, Files: models.NewRelatedVideoFiles([]*models.VideoFile{file})}
			original, err := mgr.generatePreviewImage(context.Background(), &scene, "cover", "", at)
			if err != nil {
				t.Fatal(err)
			}
			scene.CoverChecksum = md5.FromBytes(original)
			store := mgr.PreviewImageStore()
			manifest := mgr.ScenePreviewImage(&scene)
			if manifest == nil {
				t.Fatal("original cover was not published")
			}
			for _, variant := range manifest.Variants {
				path, _ := store.File(scene.ID, "cover", manifest, variant.File)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if mgr.ScenePreviewImage(&scene) != nil {
				t.Fatal("advertised deleted renditions")
			}
			db.Scene.On("Find", mock.Anything, scene.ID).Return(&scene, nil)
			db.Scene.On("UpdateCover", mock.Anything, scene.ID, mock.Anything).Run(func(args mock.Arguments) {
				data := args.Get(2).([]byte)
				if !bytes.Equal(data, original) {
					t.Error("regeneration changed the selected frame")
				}
				scene.CoverChecksum = md5.FromBytes(data)
			}).Return(nil).Once()
			db.Scene.On("UpdatePartial", mock.Anything, scene.ID, mock.Anything).Return(&scene, nil).Once()
			id := mgr.GenerateScreenshot(context.Background(), "1", at)
			if state := mgr.JobManager.GetJob(id); state.Status != job.StatusFinished {
				t.Fatalf("regeneration failed: %+v", state)
			}
			if manifest := mgr.ScenePreviewImage(&scene); manifest == nil || manifest.At != at {
				t.Fatalf("generation lost the requested timestamp: %v", manifest)
			}

			db.Scene.On("UpdateCover", mock.Anything, scene.ID, mock.Anything).Return(errors.New("cover write failed")).Once()
			id = mgr.GenerateScreenshot(context.Background(), "1", at)
			if state := mgr.JobManager.GetJob(id); state.Status != job.StatusFailed || state.Error == nil || !strings.Contains(*state.Error, "cover write failed") {
				t.Fatalf("cover write failure was reported as success: %+v", state)
			}

			// The legacy default operation still selects 20% of the duration,
			// even when the current cover was generated at a different frame.
			db.Scene.On("UpdateCover", mock.Anything, scene.ID, mock.Anything).Run(func(args mock.Arguments) {
				scene.CoverChecksum = md5.FromBytes(args.Get(2).([]byte))
			}).Return(nil).Once()
			db.Scene.On("UpdatePartial", mock.Anything, scene.ID, mock.Anything).Return(&scene, nil).Once()
			id = mgr.GenerateDefaultScreenshot(context.Background(), "1")
			if state := mgr.JobManager.GetJob(id); state.Status != job.StatusFinished {
				t.Fatalf("default generation failed: %+v", state)
			}
			if manifest := mgr.ScenePreviewImage(&scene); manifest == nil || manifest.At != file.Duration*0.2 {
				t.Fatalf("legacy default generation did not select the default frame: %v", manifest)
			}
			db.AssertExpectations(t)
		})
	}
}
