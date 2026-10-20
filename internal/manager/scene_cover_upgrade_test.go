package manager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func coverUpgradeTestManager(t *testing.T) *Manager {
	t.Helper()
	m := defaultFilterTestManager(t, "")
	p := paths.NewPaths(t.TempDir(), "")
	m.Paths = &p
	m.Database.SetBlobStoreOptions(sqlite.BlobStoreOptions{UseDatabase: true})
	return m
}

func addCoverUpgradeScene(t *testing.T, m *Manager, at float64) (*models.Scene, *models.VideoFile) {
	t.Helper()
	_, f, _ := coverSourceFixture(t)
	f.ID = 0
	f.Basename = filepath.Base(f.Path)
	s := models.NewScene()
	r := m.Repository
	require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
		folder := &models.Folder{Path: filepath.Dir(f.Path)}
		require.NoError(t, r.Folder.Create(ctx, folder))
		f.ParentFolderID = folder.ID
		require.NoError(t, r.File.Create(ctx, f))
		require.NoError(t, r.Scene.Create(ctx, &s, []models.FileID{f.ID}))
		require.NoError(t, r.Scene.UpdateCover(ctx, s.ID, []byte("selected cover")))
		scene, err := r.Scene.Find(ctx, s.ID)
		require.NoError(t, err)
		s = *scene
		// The SQL migration seeds this fixed cohort. Workflow tests start at
		// that boundary; the actual version upgrade has separate SQLite tests.
		_, _, err = m.Database.ExecSQL(ctx, `INSERT INTO scene_cover_upgrade(scene_id,cover_checksum) SELECT id,cover_blob FROM scenes WHERE id=?`, []any{s.ID})
		return err
	}))
	saveCoverTestMetadata(t, m, &s, at)
	return &s, f
}

func coverUpgradeCount(t *testing.T, m *Manager, table string) int64 {
	t.Helper()
	var ret int64
	require.NoError(t, m.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, rows, err := m.Database.QuerySQL(ctx, "SELECT count(*) FROM "+table, nil)
		require.NoError(t, err)
		ret = rows[0][0].(int64)
		return nil
	}))
	return ret
}

func reopenCoverUpgrade(t *testing.T, m *Manager) {
	t.Helper()
	path := m.Database.DatabasePath()
	require.NoError(t, m.Database.Close())
	require.NoError(t, m.Database.Open(path))
	m.Repository = m.Database.Repository()
}

func TestCoverUpgradeSurvivesCacheDeletionAndRestart(t *testing.T) {
	for _, at := range []float64{0, 12.345} {
		t.Run(fmt.Sprintf("at_%g", at), func(t *testing.T) {
			m := coverUpgradeTestManager(t)
			s, f := addCoverUpgradeScene(t, m, at)
			manifest := m.ScenePreviewImage(s)
			require.NotNil(t, manifest)
			rendition, variant := m.PreviewImageStore().File(s.ID, "cover", manifest, manifest.Variants[0].File)
			require.NotNil(t, variant)
			require.NoError(t, os.Remove(rendition), "authored evidence remains valid without renditions")
			require.NoError(t, m.UpgradeSceneCoverSelections(t.Context()))
			require.Zero(t, coverUpgradeCount(t, m, "scene_cover_upgrade"))
			require.Zero(t, coverUpgradeCount(t, m, "scene_cover_claims"))
			require.NoError(t, os.RemoveAll(m.PreviewImageStore().Root))
			for range 2 {
				reopenCoverUpgrade(t, m)
				require.NoError(t, m.UpgradeSceneCoverSelections(t.Context()))
				origin, err := m.SceneCoverOrigin(t.Context(), s)
				require.NoError(t, err)
				require.Equal(t, CoverSourceAvailable, origin.Status)
				require.Equal(t, at, origin.Source.At)
				require.Equal(t, f.ID, origin.Source.FileID)
			}
		})
	}
}

func TestCoverUpgradeOfflineEvidenceIsCapturedOnce(t *testing.T) {
	m := coverUpgradeTestManager(t)
	s, f := addCoverUpgradeScene(t, m, 12.345)
	hidden := f.Path + ".offline"
	require.NoError(t, os.Rename(f.Path, hidden))
	require.NoError(t, m.UpgradeSceneCoverSelections(t.Context()))
	require.EqualValues(t, 1, coverUpgradeCount(t, m, "scene_cover_claims"))
	origin, err := m.SceneCoverOrigin(t.Context(), s)
	require.NoError(t, err)
	require.Equal(t, CoverSourceUnresolved, origin.Status)
	// A cache wipe must not discard offline evidence or turn it into a default.
	require.NoError(t, os.RemoveAll(m.PreviewImageStore().Root))
	reopenCoverUpgrade(t, m)
	require.NoError(t, m.UpgradeSceneCoverSelections(t.Context()))
	require.EqualValues(t, 1, coverUpgradeCount(t, m, "scene_cover_claims"))
	previous := instance
	instance = m
	t.Cleanup(func() { instance = previous })
	task := &GenerateCoverTask{repository: m.Repository, Scene: *s, Overwrite: true}
	require.ErrorContains(t, task.generateWithCoverSource(t.Context()), "kept existing cover")
	require.NoError(t, os.Rename(hidden, f.Path))
	saveCoverTestMetadata(t, m, s, 77) // Later cache writes cannot replace captured evidence.
	require.NoError(t, m.UpgradeSceneCoverSelections(t.Context()))
	origin, err = m.SceneCoverOrigin(t.Context(), s)
	require.NoError(t, err)
	require.Equal(t, CoverSourceAvailable, origin.Status)
	require.Equal(t, 12.345, origin.Source.At)
	require.Zero(t, coverUpgradeCount(t, m, "scene_cover_claims"))
}

func TestCoverUpgradeDoesNotGuessOrOverrideNativeEdits(t *testing.T) {
	for _, mode := range []string{"ambiguous", "changed source", "canonical", "same artwork", "replacement artwork", "explicit clear", "deleted"} {
		t.Run(mode, func(t *testing.T) {
			m := coverUpgradeTestManager(t)
			s, f := addCoverUpgradeScene(t, m, 12.345)
			switch mode {
			case "ambiguous":
				other, otherFile := addCoverUpgradeScene(t, m, 42)
				otherSource := *s
				otherSource.Path = otherFile.Path
				saveCoverTestMetadata(t, m, &otherSource, 42)
				require.NoError(t, m.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
					require.NoError(t, m.Repository.Scene.AssignFiles(ctx, s.ID, []models.FileID{otherFile.ID}))
					return m.Repository.Scene.Destroy(ctx, other.ID)
				}))
			case "changed source":
				require.NoError(t, os.WriteFile(f.Path, []byte("a changed source"), 0600))
			default:
				require.NoError(t, m.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
					switch mode {
					case "canonical":
						fp, err := coverFingerprint(f)
						require.NoError(t, err)
						return m.Repository.Scene.SetCoverSource(ctx, s.ID, &models.SceneCoverSource{CoverChecksum: s.CoverChecksum, FileID: f.ID, At: 77, Fingerprint: fp})
					case "same artwork":
						return m.Repository.Scene.UpdateCover(ctx, s.ID, []byte("selected cover"))
					case "replacement artwork":
						return m.Repository.Scene.UpdateCover(ctx, s.ID, []byte("replacement"))
					case "explicit clear":
						return m.Repository.Scene.SetCoverSource(ctx, s.ID, nil)
					default:
						return m.Repository.Scene.Destroy(ctx, s.ID)
					}
				}))
			}
			require.NoError(t, m.UpgradeSceneCoverSelections(t.Context()))
			reopenCoverUpgrade(t, m)
			require.NoError(t, m.UpgradeSceneCoverSelections(t.Context()))
			require.NoError(t, m.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
				saved, err := m.Repository.Scene.GetCoverSource(ctx, s.ID)
				require.NoError(t, err)
				if mode == "canonical" {
					require.Equal(t, 77.0, saved.At)
				} else {
					require.Nil(t, saved)
				}
				pending, err := m.Repository.SceneCoverUpgrade.Pending(ctx, s.ID)
				require.NoError(t, err)
				require.Equal(t, mode == "ambiguous" || mode == "changed source", pending)
				return nil
			}))
			if mode != "ambiguous" && mode != "changed source" {
				require.Zero(t, coverUpgradeCount(t, m, "scene_cover_upgrade"))
				require.Zero(t, coverUpgradeCount(t, m, "scene_cover_claims"))
			}
		})
	}
}

func TestCoverUpgradeBatchFailureRollsBackAndRetries(t *testing.T) {
	m := coverUpgradeTestManager(t)
	var scenes []*models.Scene
	for i := range 51 {
		scene, _ := addCoverUpgradeScene(t, m, float64(i))
		scenes = append(scenes, scene)
	}
	require.NoError(t, m.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := m.Database.ExecSQL(ctx, fmt.Sprintf(`CREATE TRIGGER fail_cover_capture BEFORE INSERT ON scene_cover_claims
WHEN NEW.scene_id=%d BEGIN SELECT RAISE(ABORT,'injected capture failure'); END`, scenes[50].ID), nil)
		return err
	}))
	require.ErrorContains(t, m.UpgradeSceneCoverSelections(t.Context()), "injected capture failure")
	require.EqualValues(t, 50, coverUpgradeCount(t, m, "scene_cover_sources"))
	require.EqualValues(t, 1, coverUpgradeCount(t, m, "scene_cover_upgrade"))
	require.Zero(t, coverUpgradeCount(t, m, "scene_cover_claims"))
	require.NoError(t, m.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := m.Database.ExecSQL(ctx, "DROP TRIGGER fail_cover_capture", nil)
		return err
	}))
	reopenCoverUpgrade(t, m)
	require.NoError(t, m.UpgradeSceneCoverSelections(t.Context()))
	require.EqualValues(t, 51, coverUpgradeCount(t, m, "scene_cover_sources"))
	require.Zero(t, coverUpgradeCount(t, m, "scene_cover_upgrade"))
	require.Zero(t, coverUpgradeCount(t, m, "scene_cover_claims"))
}

func TestCoverUpgradeMissingGeneratedRootRetainsCohort(t *testing.T) {
	m := coverUpgradeTestManager(t)
	s, _ := addCoverUpgradeScene(t, m, 12)
	root := filepath.Dir(m.PreviewImageStore().Root)
	hidden := root + "-offline"
	require.NoError(t, os.Rename(root, hidden))
	t.Cleanup(func() { _ = os.Rename(hidden, root) })
	require.Error(t, m.UpgradeSceneCoverSelections(t.Context()))
	require.EqualValues(t, 1, coverUpgradeCount(t, m, "scene_cover_upgrade"))
	require.NoError(t, os.Rename(hidden, root))
	require.NoError(t, m.UpgradeSceneCoverSelections(t.Context()))
	origin, err := m.SceneCoverOrigin(t.Context(), s)
	require.NoError(t, err)
	require.Equal(t, 12.0, origin.Source.At)
}

func TestCoverUpgradeMetadataEditsRetainAuthoredEvidence(t *testing.T) {
	m := coverUpgradeTestManager(t)
	s, _ := addCoverUpgradeScene(t, m, 12)
	s.Title = "edited title"
	require.NoError(t, m.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
		return m.Repository.Scene.Update(ctx, s)
	}))
	require.EqualValues(t, 1, coverUpgradeCount(t, m, "scene_cover_upgrade"))
	require.NoError(t, m.UpgradeSceneCoverSelections(t.Context()))
	origin, err := m.SceneCoverOrigin(t.Context(), s)
	require.NoError(t, err)
	require.Equal(t, CoverSourceAvailable, origin.Status)
	require.Equal(t, 12.0, origin.Source.At)
	s.Title = "edited again after capture"
	require.NoError(t, m.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
		return m.Repository.Scene.Update(ctx, s)
	}))
	origin, err = m.SceneCoverOrigin(t.Context(), s)
	require.NoError(t, err)
	require.Equal(t, CoverSourceAvailable, origin.Status)
	require.Equal(t, 12.0, origin.Source.At)
}

func TestCoverUpgradeCheckpointRequiresCaptureButAllowsOfflineClaims(t *testing.T) {
	m := coverUpgradeTestManager(t)
	_, f := addCoverUpgradeScene(t, m, 12)
	path := m.Database.DatabasePath()
	called := false
	capture := func(*sqlite.NativeCheckpoint) error { called = true; return nil }
	require.ErrorContains(t, sqlite.WithNativeCheckpoint(t.Context(), path, capture), "cover selection evidence")
	require.False(t, called)
	require.NoError(t, m.Database.Close())
	_, err := sqlite.VerifyNativeSnapshot(t.Context(), path)
	require.ErrorContains(t, err, "uncaptured cover selection evidence")
	require.NoError(t, m.Database.Open(path))
	m.Repository = m.Database.Repository()
	require.NoError(t, os.Remove(f.Path))
	require.NoError(t, m.UpgradeSceneCoverSelections(t.Context()))
	require.NoError(t, sqlite.WithNativeCheckpoint(t.Context(), path, capture))
	require.True(t, called)
	require.NoError(t, m.Database.Close())
	_, err = sqlite.VerifyNativeSnapshot(t.Context(), path)
	require.NoError(t, err)
	require.NoError(t, m.Database.Open(path))
	m.Repository = m.Database.Repository()
	require.EqualValues(t, 1, coverUpgradeCount(t, m, "scene_cover_claims"))
}
