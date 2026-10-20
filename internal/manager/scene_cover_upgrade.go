package manager

// LEGACY-UPGRADE-ONLY: schema 1000125 freezes the existing scenes that may have
// authored selections in original v1 preview manifests. Capture those claims
// before ordinary jobs can clean the cache. Completed rows are removed, never
// recreated; offline or ambiguous claims remain in SQLite for a later startup.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stashapp/stash/pkg/covermigration"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
)

func (s *Manager) UpgradeSceneCoverSelections(ctx context.Context) error {
	r := s.Repository
	after, pending := 0, 0
	for {
		var batch []models.SceneCoverUpgrade
		err := r.WithTxn(ctx, func(ctx context.Context) error {
			var err error
			batch, err = r.SceneCoverUpgrade.Page(ctx, after)
			if err != nil {
				return err
			}
			for _, upgrade := range batch {
				unresolved, err := s.upgradeSceneCoverSelection(ctx, upgrade)
				if err != nil {
					return fmt.Errorf("upgrading scene %d cover selection: %w", upgrade.SceneID, err)
				}
				if unresolved {
					pending++
				}
			}
			return nil
		})
		if err != nil || len(batch) == 0 {
			if err == nil && pending > 0 {
				logger.Warnf("Retained unresolved cover selection evidence for %d scenes; existing covers are preserved", pending)
			}
			return err
		}
		after = batch[len(batch)-1].SceneID
	}
}

func (s *Manager) upgradeSceneCoverSelection(ctx context.Context, upgrade models.SceneCoverUpgrade) (bool, error) {
	r, id := s.Repository, upgrade.SceneID
	scene, err := r.Scene.Find(ctx, id)
	if err != nil {
		return false, err
	}
	source, err := r.Scene.GetCoverSource(ctx, id)
	if err != nil {
		return false, err
	}
	if scene == nil || scene.CoverChecksum != upgrade.Checksum || source != nil {
		return false, r.SceneCoverUpgrade.Discard(ctx, id)
	}
	claims, err := r.SceneCoverUpgrade.Claims(ctx, id)
	if err != nil {
		return false, err
	}
	if !upgrade.Captured {
		root := s.PreviewImageStore().Root
		// A missing preview subdirectory can be an empty cache; an unavailable
		// generated root must not silently discard the upgrade cohort.
		info, err := os.Stat(filepath.Dir(root))
		if err != nil {
			return false, err
		}
		if !info.IsDir() {
			return false, fmt.Errorf("generated root is not a directory")
		}
		claims, err = covermigration.Claims(ctx, root, id)
		if err != nil {
			return false, err
		}
		if err := r.SceneCoverUpgrade.Capture(ctx, id, claims); err != nil {
			return false, err
		}
	}
	if len(claims) == 0 {
		return false, r.SceneCoverUpgrade.Discard(ctx, id)
	}
	files, err := r.Scene.GetFiles(ctx, id)
	if err != nil {
		return false, err
	}
	unavailable := false
	for _, file := range files {
		fingerprint, err := coverFingerprint(file)
		if err != nil {
			unavailable = true
			continue
		}
		key := covermigration.SourceKey(file.Path, fingerprint.Size, fingerprint.ModTimeNano, scene.CoverChecksum)
		for _, claim := range claims {
			if claim.Key != key || !validCoverTime(claim.At, file.Duration) {
				continue
			}
			if source != nil {
				return true, nil // More than one attached source matches: do not guess.
			}
			source = &models.SceneCoverSource{CoverChecksum: scene.CoverChecksum, FileID: file.ID, At: claim.At, Fingerprint: fingerprint}
		}
	}
	if source == nil || unavailable {
		return true, nil
	}
	// Inserting the native source also prunes its upgrade evidence atomically.
	return false, r.Scene.SetCoverSource(ctx, id, source)
}
