package manager

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/hash/md5"
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
	for _, mode := range []string{"changed", "missing", "detached"} {
		t.Run(mode, func(t *testing.T) {
			mgr, db := coverTestManager(t)
			previous := instance
			instance = mgr
			t.Cleanup(func() { instance = previous })
			scene, file, source := coverSourceFixture(t)
			files := scene.Files.List()
			switch mode {
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
	scene, file, source := coverSourceFixture(t)
	require.NoError(t, os.Remove(file.Path))
	other := *scene
	other.ID = 2
	scenes := []*models.Scene{scene, &other}
	db.Scene.On("FindMany", mock.Anything, []int{1, 2}).Return(scenes, nil)
	for _, scene := range scenes {
		db.Scene.On("Find", mock.Anything, scene.ID).Return(scene, nil)
		db.Scene.On("GetFiles", mock.Anything, scene.ID).Return(scene.Files.List(), nil)
		db.Scene.On("GetCoverSource", mock.Anything, scene.ID).Return(source, nil)
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

func TestBulkCoverDefaultSelection(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required for cover generation")
	}
	probePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is required for cover generation")
	}
	path := filepath.Join(t.TempDir(), "source.mkv")
	output, err := exec.Command(ffmpegPath, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=64x48:rate=8:duration=3", "-c:v", "ffv1", path).CombinedOutput()
	require.NoError(t, err, string(output))
	for _, tc := range []struct {
		name      string
		recorded  bool
		at        float64
		reset     bool
		overwrite bool
		detached  bool
		want      float64
	}{
		{name: "unknown falls back to default", overwrite: true, want: 0.6},
		{name: "recorded frame is preserved", recorded: true, at: 1.125, overwrite: true, want: 1.125},
		{name: "recorded zero is preserved", recorded: true, at: 0, overwrite: true, want: 0},
		{name: "reset overrides recorded frame without global overwrite", recorded: true, at: 1.125, reset: true, want: 0.6},
		{name: "reset uses primary instead of detached source", recorded: true, at: 1.125, reset: true, detached: true, want: 0.6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, db := coverTestManager(t)
			mgr.FFMpeg, mgr.FFProbe = ffmpeg.NewEncoder(ffmpegPath), ffmpeg.NewFFProbe(probePath)
			previous := instance
			instance = mgr
			t.Cleanup(func() { instance = previous })
			file := &models.VideoFile{BaseFile: &models.BaseFile{ID: 1, Path: path}, Width: 64, Height: 48, Duration: 3}
			scene := &models.Scene{ID: 1, Path: path, CoverChecksum: "old-cover", Files: models.NewRelatedVideoFiles([]*models.VideoFile{file})}
			var saved *models.SceneCoverSource
			if tc.recorded {
				fingerprint, err := coverFingerprint(file)
				require.NoError(t, err)
				saved = &models.SceneCoverSource{CoverChecksum: scene.CoverChecksum, FileID: file.ID, At: tc.at, Fingerprint: fingerprint}
				if tc.detached {
					saved.FileID = 99
				}
			}
			db.Scene.On("FindMany", mock.Anything, []int{1}).Return([]*models.Scene{scene}, nil)
			db.Scene.On("Find", mock.Anything, scene.ID).Return(scene, nil)
			db.Scene.On("GetFiles", mock.Anything, scene.ID).Return(scene.Files.List(), nil)
			db.Scene.On("GetCoverSource", mock.Anything, scene.ID).Return(func(context.Context, int) *models.SceneCoverSource { return saved }, nil)
			db.Scene.On("UpdateCover", mock.Anything, scene.ID, mock.Anything).Run(func(args mock.Arguments) {
				scene.CoverChecksum = md5.FromBytes(args.Get(2).([]byte))
			}).Return(nil).Once()
			db.Scene.On("SetCoverSource", mock.Anything, scene.ID, mock.Anything).Run(func(args mock.Arguments) {
				selection := *args.Get(2).(*models.SceneCoverSource)
				saved = &selection
			}).Return(nil).Once()
			db.Scene.On("UpdatePartial", mock.Anything, scene.ID, mock.Anything).Return(scene, nil).Once()
			j := &GenerateJob{repository: db.Repository(), input: GenerateMetadataInput{SceneIDs: []string{"1"}, Covers: true, Overwrite: tc.overwrite, ResetCoversToDefault: tc.reset}}
			id := mgr.JobManager.Add(context.Background(), "Generate covers", j)
			state := mgr.JobManager.GetJob(id)
			require.Equal(t, job.StatusFinished, state.Status, "%+v", state.Error)
			require.NotNil(t, saved)
			require.InDelta(t, tc.want, saved.At, 0.000001)
			require.Equal(t, file.ID, saved.FileID)
			require.Equal(t, scene.CoverChecksum, saved.CoverChecksum)
			require.Equal(t, CoverSourceAvailable, coverSourceStatus(scene, saved))
			manifest := mgr.ScenePreviewImage(scene)
			require.NotNil(t, manifest)
			require.InDelta(t, tc.want, manifest.At, 0.000001)
			require.NotEmpty(t, manifest.Thumbnail)
			db.AssertExpectations(t)
		})
	}
}

func TestResetCoverKeepsArtworkWhenPrimaryIsMissing(t *testing.T) {
	mgr, db := coverTestManager(t)
	previous := instance
	instance = mgr
	t.Cleanup(func() { instance = previous })
	scene, file, source := coverSourceFixture(t)
	require.NoError(t, os.Remove(file.Path))
	db.Scene.On("Find", mock.Anything, scene.ID).Return(scene, nil)
	db.Scene.On("GetFiles", mock.Anything, scene.ID).Return(scene.Files.List(), nil)
	db.Scene.On("GetCoverSource", mock.Anything, scene.ID).Return(source, nil)
	task := GenerateCoverTask{repository: db.Repository(), Scene: *scene, Overwrite: true, ResetToDefault: true}
	require.Error(t, task.generate(context.Background()))
	require.Equal(t, "cover", scene.CoverChecksum)
	require.Equal(t, 12.345, source.At)
	db.Scene.AssertNotCalled(t, "UpdateCover", mock.Anything, mock.Anything, mock.Anything)
	db.Scene.AssertNotCalled(t, "SetCoverSource", mock.Anything, mock.Anything, mock.Anything)
}
