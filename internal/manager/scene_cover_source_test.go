package manager

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/previewimage"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func coverSourceFixture(t *testing.T) (*models.Scene, *models.VideoFile, *models.SceneCoverSource) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "video.mkv")
	require.NoError(t, os.WriteFile(path, []byte("original source"), 0600))
	stat, err := os.Stat(path)
	require.NoError(t, err)
	file := &models.VideoFile{BaseFile: &models.BaseFile{ID: 1, Path: path, Size: stat.Size(), DirEntry: models.DirEntry{ModTime: stat.ModTime()},
		Fingerprints: models.Fingerprints{{Type: models.FingerprintTypeMD5, Fingerprint: "original"}}}, Duration: 100}
	scene := &models.Scene{ID: 1, Path: path, CoverChecksum: "cover", Files: models.NewRelatedVideoFiles([]*models.VideoFile{file})}
	fingerprint, err := coverFingerprint(file)
	require.NoError(t, err)
	source := &models.SceneCoverSource{CoverChecksum: scene.CoverChecksum, FileID: file.ID, At: 12.345, Fingerprint: fingerprint}
	return scene, file, source
}

func TestSceneCoverSourceValidity(t *testing.T) {
	tests := []struct {
		name   string
		change func(*testing.T, *models.Scene, *models.VideoFile, *models.SceneCoverSource)
		want   CoverSourceStatus
	}{
		{"unchanged", func(*testing.T, *models.Scene, *models.VideoFile, *models.SceneCoverSource) {}, CoverSourceAvailable},
		{"rename", func(t *testing.T, s *models.Scene, f *models.VideoFile, _ *models.SceneCoverSource) {
			path := filepath.Join(filepath.Dir(f.Path), "renamed.mkv")
			require.NoError(t, os.Rename(f.Path, path))
			f.Path, s.Path = path, path
		}, CoverSourceAvailable},
		{"extra scan hashes", func(_ *testing.T, _ *models.Scene, f *models.VideoFile, _ *models.SceneCoverSource) {
			f.Fingerprints = append(f.Fingerprints, models.Fingerprint{Type: models.FingerprintTypePhash, Fingerprint: int64(123)}, models.Fingerprint{Type: models.FingerprintTypeOshash, Fingerprint: "new"})
		}, CoverSourceAvailable},
		{"rescan replacement", func(_ *testing.T, _ *models.Scene, f *models.VideoFile, _ *models.SceneCoverSource) {
			f.Fingerprints[0].Fingerprint = "different"
		}, CoverSourceChanged},
		{"in place edit before scan", func(t *testing.T, _ *models.Scene, f *models.VideoFile, _ *models.SceneCoverSource) {
			require.NoError(t, os.WriteFile(f.Path, []byte("replacement source"), 0600))
		}, CoverSourceChanged},
		{"same size edit", func(t *testing.T, _ *models.Scene, f *models.VideoFile, _ *models.SceneCoverSource) {
			require.NoError(t, os.Chtimes(f.Path, time.Now(), f.ModTime.Add(time.Second)))
		}, CoverSourceChanged},
		{"unavailable", func(t *testing.T, _ *models.Scene, f *models.VideoFile, _ *models.SceneCoverSource) {
			require.NoError(t, os.Remove(f.Path))
		}, CoverSourceUnavailable},
		{"detached", func(_ *testing.T, s *models.Scene, _ *models.VideoFile, _ *models.SceneCoverSource) {
			s.Files = models.NewRelatedVideoFiles([]*models.VideoFile{})
		}, CoverSourceChanged},
		{"new file same scene", func(_ *testing.T, _ *models.Scene, f *models.VideoFile, _ *models.SceneCoverSource) { f.ID++ }, CoverSourceChanged},
		{"out of range", func(_ *testing.T, _ *models.Scene, f *models.VideoFile, _ *models.SceneCoverSource) { f.Duration = 10 }, CoverSourceChanged},
		{"zero", func(_ *testing.T, _ *models.Scene, _ *models.VideoFile, s *models.SceneCoverSource) { s.At = 0 }, CoverSourceAvailable},
		{"invalid", func(_ *testing.T, _ *models.Scene, _ *models.VideoFile, s *models.SceneCoverSource) {
			s.At = math.NaN()
		}, CoverSourceChanged},
		{"unsupported fingerprint", func(_ *testing.T, _ *models.Scene, _ *models.VideoFile, s *models.SceneCoverSource) {
			s.Fingerprint.Version++
		}, CoverSourceChanged},
		{"replaced artwork", func(_ *testing.T, s *models.Scene, _ *models.VideoFile, _ *models.SceneCoverSource) {
			s.CoverChecksum = "upload"
		}, CoverSourceChanged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scene, file, source := coverSourceFixture(t)
			tt.change(t, scene, file, source)
			require.Equal(t, tt.want, coverSourceStatus(scene, source))
		})
	}
}

func TestCoverFingerprintUsesScanTimestampPrecision(t *testing.T) {
	_, file, source := coverSourceFixture(t)
	file.ModTime = file.ModTime.Truncate(time.Second)
	fingerprint, err := coverFingerprint(file)
	require.NoError(t, err)
	require.Equal(t, source.Fingerprint, fingerprint)
	file.ModTime = file.ModTime.Add(-time.Second)
	fingerprint, err = coverFingerprint(file)
	require.NoError(t, err)
	require.Empty(t, fingerprint.MD5, "a stale scan hash must not be captured for a new selection")
}

func TestRegenerateSceneCoverKeepsUnreproducibleArtwork(t *testing.T) {
	for _, mode := range []string{"unknown", "changed", "missing", "detached"} {
		t.Run(mode, func(t *testing.T) {
			mgr, db := coverTestManager(t)
			previous := instance
			instance = mgr
			t.Cleanup(func() { instance = previous })
			scene, file, source := coverSourceFixture(t)
			files := scene.Files.List()
			switch mode {
			case "unknown":
				source = nil
			case "changed":
				require.NoError(t, os.WriteFile(file.Path, []byte("new source"), 0600))
			case "missing":
				require.NoError(t, os.Remove(file.Path))
			case "detached":
				files = []*models.VideoFile{}
			}
			db.Scene.On("Find", mock.Anything, scene.ID).Return(scene, nil)
			db.Scene.On("GetFiles", mock.Anything, scene.ID).Return(files, nil)
			db.Scene.On("GetCoverSource", mock.Anything, scene.ID).Return(source, nil)
			id := mgr.RegenerateSceneCover(context.Background(), "1")
			state := mgr.JobManager.GetJob(id)
			require.Equal(t, job.StatusFailed, state.Status)
			require.NotNil(t, state.Error)
			require.Contains(t, *state.Error, "kept existing cover")
			require.Contains(t, *state.Error, "select a new cover frame")
			require.Equal(t, "cover", scene.CoverChecksum)
			db.Scene.AssertNotCalled(t, "UpdateCover", mock.Anything, mock.Anything, mock.Anything)
			db.Scene.AssertNotCalled(t, "SetCoverSource", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func TestLegacyCoverOriginRecoversWithoutRenditions(t *testing.T) {
	for _, at := range []float64{0, 12.345} {
		t.Run(fmt.Sprintf("at_%g", at), func(t *testing.T) {
			mgr, db := coverTestManager(t)
			scene, _, _ := coverSourceFixture(t)
			saveCoverTestMetadata(t, mgr, scene, at)
			manifest := mgr.ScenePreviewImage(scene)
			require.NotNil(t, manifest)
			file, _ := mgr.PreviewImageStore().File(scene.ID, "cover", manifest, manifest.Variants[0].File)
			require.NoError(t, os.Remove(file))
			db.Scene.On("GetFiles", mock.Anything, scene.ID).Return(scene.Files.List(), nil)
			db.Scene.On("GetCoverSource", mock.Anything, scene.ID).Return(nil, nil)
			origin, err := mgr.SceneCoverOrigin(context.Background(), scene)
			require.NoError(t, err)
			require.Equal(t, CoverSourceAvailable, origin.Status)
			require.Equal(t, at, origin.Source.At)
			// A replacement source cannot inherit the timestamp from that cache.
			require.NoError(t, os.WriteFile(scene.Path, []byte("replacement"), 0600))
			origin, err = mgr.SceneCoverOrigin(context.Background(), scene)
			require.NoError(t, err)
			require.Equal(t, CoverSourceUnknown, origin.Status)
		})
	}
}

func TestSavedCoverRenditionsSurviveSourceRemoval(t *testing.T) {
	mgr, _ := coverTestManager(t)
	scene, _, _ := coverSourceFixture(t)
	result := &previewimage.Result{Directory: t.TempDir(), Variants: []previewimage.Variant{{File: "preview.jpg", MIMEType: "image/jpeg", DynamicRange: previewimage.SDR, Width: 64, Height: 48}}}
	require.NoError(t, os.WriteFile(filepath.Join(result.Directory, "preview.jpg"), []byte("cover"), 0600))
	require.NoError(t, mgr.PreviewImageStore().Publish(scene.ID, "cover", previewimage.CoverKey(scene.CoverChecksum), 12.345, result))
	require.NoError(t, os.Remove(scene.Path))
	require.NotNil(t, mgr.ScenePreviewImage(scene))
	scene.CoverChecksum = "upstream replacement"
	require.Nil(t, mgr.ScenePreviewImage(scene))
}

func TestBulkCoverGenerationReportsRetainedCovers(t *testing.T) {
	mgr, db := coverTestManager(t)
	previous := instance
	instance = mgr
	t.Cleanup(func() { instance = previous })
	scene, _, _ := coverSourceFixture(t)
	other := *scene
	other.ID = 2
	scenes := []*models.Scene{scene, &other}
	db.Scene.On("FindMany", mock.Anything, []int{1, 2}).Return(scenes, nil)
	for _, scene := range scenes {
		db.Scene.On("Find", mock.Anything, scene.ID).Return(scene, nil)
		db.Scene.On("GetFiles", mock.Anything, scene.ID).Return(scene.Files.List(), nil)
		db.Scene.On("GetCoverSource", mock.Anything, scene.ID).Return(nil, nil)
	}
	j := &GenerateJob{repository: db.Repository(), input: GenerateMetadataInput{SceneIDs: []string{"1", "2"}, Covers: true, Overwrite: true}}
	id := mgr.JobManager.Add(context.Background(), "Regenerate scene covers", j)
	state := mgr.JobManager.GetJob(id)
	require.Equal(t, job.StatusFailed, state.Status)
	require.NotNil(t, state.Error)
	require.Contains(t, *state.Error, "2 scene covers could not be regenerated")
	require.Contains(t, *state.Error, "select a new cover frame")
	db.AssertExpectations(t)
}
