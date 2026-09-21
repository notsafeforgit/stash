package manager

import (
	"context"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/stashapp/stash/pkg/hash/md5"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scene/generate"
)

type CoverSourceStatus string

const (
	CoverSourceAvailable   CoverSourceStatus = "AVAILABLE"
	CoverSourceChanged     CoverSourceStatus = "CHANGED"
	CoverSourceUnavailable CoverSourceStatus = "UNAVAILABLE"
	CoverSourceUnknown     CoverSourceStatus = "UNKNOWN"
)

type CoverOrigin struct {
	Source *models.SceneCoverSource
	Status CoverSourceStatus
}

func coverFingerprint(file *models.VideoFile) (models.SceneCoverFingerprint, error) {
	stat, err := os.Stat(file.Path)
	if err != nil {
		return models.SceneCoverFingerprint{}, err
	}
	if !stat.Mode().IsRegular() {
		return models.SceneCoverFingerprint{}, fmt.Errorf("cover source is not a regular file")
	}
	ret := models.SceneCoverFingerprint{Version: 1, Size: stat.Size(), ModTimeNano: stat.ModTime().UnixNano()}
	// Do not associate an explicitly selected frame with hashes from a scan
	// that predates an in-place edit. The filesystem fingerprint still applies.
	// The upstream file table stores modification times to whole seconds.
	if file.Size == stat.Size() && file.ModTime.Truncate(time.Second).Equal(stat.ModTime().Truncate(time.Second)) {
		ret.MD5 = file.Fingerprints.GetString(models.FingerprintTypeMD5)
		ret.OSHash = file.Fingerprints.GetString(models.FingerprintTypeOshash)
	}
	return ret, nil
}

func validCoverTime(at, duration float64) bool {
	return at >= 0 && at < duration && !math.IsNaN(at) && !math.IsInf(at, 0) && !math.IsInf(duration, 0)
}

func coverSourceFile(scene *models.Scene, id models.FileID) *models.VideoFile {
	for _, file := range scene.Files.List() {
		if file.ID == id {
			return file
		}
	}
	return nil
}

func coverSourceStatus(scene *models.Scene, source *models.SceneCoverSource) CoverSourceStatus {
	if source.Validate() != nil || scene.CoverChecksum != source.CoverChecksum {
		return CoverSourceChanged
	}
	file := coverSourceFile(scene, source.FileID)
	if file == nil {
		return CoverSourceChanged
	}
	current, err := coverFingerprint(file)
	if err != nil {
		return CoverSourceUnavailable
	}
	saved := source.Fingerprint
	if saved.Size != current.Size || saved.ModTimeNano != current.ModTimeNano || !validCoverTime(source.At, file.Duration) ||
		(saved.MD5 != "" && saved.MD5 != file.Fingerprints.GetString(models.FingerprintTypeMD5)) ||
		(saved.OSHash != "" && saved.OSHash != file.Fingerprints.GetString(models.FingerprintTypeOshash)) {
		return CoverSourceChanged
	}
	return CoverSourceAvailable
}

// coverOrigin assumes loaded scene files. Reading a
// legacy manifest is a compatibility bridge, never a write during a query.
func (s *Manager) coverOrigin(scene *models.Scene, source *models.SceneCoverSource) *CoverOrigin {
	if scene.CoverChecksum == "" {
		return nil
	}
	if source == nil {
		for _, file := range scene.Files.List() {
			fingerprint, err := coverFingerprint(file)
			if err != nil {
				continue
			}
			at, err := s.PreviewImageStore().LegacyCoverTimestamp(scene.ID, file.Path, scene.CoverChecksum)
			if err != nil || !validCoverTime(at, file.Duration) {
				continue
			}
			source = &models.SceneCoverSource{CoverChecksum: scene.CoverChecksum, FileID: file.ID, At: at, Fingerprint: fingerprint}
			break
		}
	}
	if source == nil {
		return &CoverOrigin{Status: CoverSourceUnknown}
	}
	return &CoverOrigin{Source: source, Status: coverSourceStatus(scene, source)}
}

func (s *Manager) SceneCoverOrigin(ctx context.Context, scene *models.Scene) (*CoverOrigin, error) {
	if !s.Config.GetEnableV3UI() || scene.CoverChecksum == "" {
		return nil, nil
	}
	// Resolver objects can be shared by parallel GraphQL fields.
	resolved := *scene
	var ret *CoverOrigin
	err := s.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
		files, err := s.Repository.Scene.GetFiles(ctx, scene.ID)
		if err != nil {
			return err
		}
		resolved.Files = models.NewRelatedVideoFiles(files)
		source, err := s.Repository.Scene.GetCoverSource(ctx, scene.ID)
		if err != nil {
			return err
		}
		ret = s.coverOrigin(&resolved, source)
		return nil
	})
	return ret, err
}

func (s *Manager) RegenerateSceneCover(ctx context.Context, sceneID string) int {
	return s.generateScreenshot(ctx, sceneID, nil, true)
}

func retainedCoverError(sceneID int, status CoverSourceStatus) error {
	reason := "the original frame is unknown"
	switch status {
	case CoverSourceChanged:
		reason = "the original video has changed or is no longer attached"
	case CoverSourceUnavailable:
		reason = "the original video is unavailable"
	}
	return fmt.Errorf("scene %d: kept existing cover because %s; select a new cover frame", sceneID, reason)
}

// generateWithCoverSource keeps authored selection separate from rendition
// recipes. Only explicit frame/default actions may replace an unknown or stale
// selection. Bulk generation and the regenerate action preserve it.
func (t *GenerateCoverTask) generateWithCoverSource(ctx context.Context) error {
	s, r := instance, t.repository
	var scene models.Scene
	var source models.SceneCoverSource
	var previousSource *models.SceneCoverSource
	var file *models.VideoFile
	var required bool
	if err := r.WithReadTxn(ctx, func(ctx context.Context) error {
		current, err := r.Scene.Find(ctx, t.Scene.ID)
		if err != nil {
			return err
		}
		if current == nil {
			return fmt.Errorf("scene %d no longer exists", t.Scene.ID)
		}
		scene = *current
		required = t.Overwrite || scene.CoverChecksum == ""
		if !required {
			return nil
		}
		files, err := r.Scene.GetFiles(ctx, scene.ID)
		if err != nil {
			return err
		}
		scene.Files = models.NewRelatedVideoFiles(files)
		if scene.CoverChecksum != "" {
			previousSource, err = r.Scene.GetCoverSource(ctx, scene.ID)
			if err != nil {
				return err
			}
		}
		if t.ScreenshotAt == nil && !t.ResetToDefault && scene.CoverChecksum != "" {
			origin := s.coverOrigin(&scene, previousSource)
			if origin.Status != CoverSourceAvailable {
				return retainedCoverError(scene.ID, origin.Status)
			}
			source = *origin.Source
			file = coverSourceFile(&scene, source.FileID)
		} else {
			file = scene.Files.Primary()
			if file == nil {
				return fmt.Errorf("scene %d has no primary video file", scene.ID)
			}
			source = models.SceneCoverSource{CoverChecksum: scene.CoverChecksum, FileID: file.ID, At: file.Duration * 0.2}
			if t.ScreenshotAt != nil {
				source.At = *t.ScreenshotAt
			}
			source.Fingerprint, err = coverFingerprint(file)
			if err != nil {
				return err
			}
		}
		if file.ID <= 0 || !validCoverTime(source.At, file.Duration) {
			return fmt.Errorf("scene %d: cover time must be within the source video duration", scene.ID)
		}
		return nil
	}); err != nil || !required {
		return err
	}

	// The selected file may still be attached without being the primary file.
	scene.Path = file.Path
	g := generate.Generator{Encoder: s.FFMpeg, FFMpegConfig: s.Config, LockManager: s.ReadLockManager, ScenePaths: s.Paths.Scene, Overwrite: true}
	data, err := s.generateCoverImage(ctx, &scene, file, source.At, g)
	if err != nil {
		return fmt.Errorf("error generating scene %d cover: %w", scene.ID, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	source.CoverChecksum = md5.FromBytes(data)
	return r.WithTxn(ctx, func(ctx context.Context) error {
		current, err := r.Scene.Find(ctx, scene.ID)
		if err != nil {
			return err
		}
		if current == nil || current.CoverChecksum != scene.CoverChecksum {
			return fmt.Errorf("scene %d cover changed during generation; kept the newer cover", scene.ID)
		}
		if current.CoverChecksum != "" {
			currentSource, err := r.Scene.GetCoverSource(ctx, scene.ID)
			if err != nil {
				return err
			}
			// Different timestamps can produce identical pixels (for example a
			// black frame). Compare the selection as well as the cover checksum.
			if !sameCoverSource(previousSource, currentSource) {
				return fmt.Errorf("scene %d cover selection changed during generation; kept the newer selection", scene.ID)
			}
		}
		files, err := r.Scene.GetFiles(ctx, scene.ID)
		if err != nil {
			return err
		}
		check := *current
		check.Files = models.NewRelatedVideoFiles(files)
		// Validate the proposed cover against the latest source association.
		// The existing cover identity was checked above, before any writes.
		check.CoverChecksum = source.CoverChecksum
		if status := coverSourceStatus(&check, &source); status != CoverSourceAvailable {
			return retainedCoverError(scene.ID, status)
		}
		if err := r.Scene.UpdateCover(ctx, scene.ID, data); err != nil {
			return fmt.Errorf("error setting screenshot: %w", err)
		}
		if err := r.Scene.SetCoverSource(ctx, scene.ID, &source); err != nil {
			return err
		}
		_, err = r.Scene.UpdatePartial(ctx, scene.ID, models.NewScenePartial())
		return err
	})
}

func sameCoverSource(a, b *models.SceneCoverSource) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
