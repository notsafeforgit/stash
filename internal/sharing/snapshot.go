package sharing

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

func (s *Service) snapshot(ctx context.Context, targets []Target) (*models.ShareSnapshot, error) {
	ret := &models.ShareSnapshot{Entries: []models.ShareEntry{}, Media: []models.ShareMedia{}}
	seen := make(map[string]bool)
	add := func(kind string, id int) (string, error) {
		key := strings.ToLower(kind) + "-" + strconv.Itoa(id)
		if seen[key] {
			return key, nil
		}
		if len(ret.Media) >= MaxItems {
			return "", fmt.Errorf("a share may contain at most %d media items", MaxItems)
		}
		var f models.File
		var title string
		switch kind {
		case "SCENE":
			v, err := s.Repo.Scene.Find(ctx, id)
			if err != nil {
				return "", err
			}
			if v == nil {
				return "", fmt.Errorf("scene %d is unavailable", id)
			}
			if err := v.LoadPrimaryFile(ctx, s.Repo.File); err != nil {
				return "", err
			}
			if v.Files.Primary() != nil {
				f = v.Files.Primary()
			}
			title = v.Title
		case "IMAGE":
			v, err := s.Repo.Image.Find(ctx, id)
			if err != nil {
				return "", err
			}
			if v == nil {
				return "", fmt.Errorf("image %d is unavailable", id)
			}
			if err := v.LoadPrimaryFile(ctx, s.Repo.File); err != nil {
				return "", err
			}
			f = v.Files.Primary()
			title = v.Title
		default:
			return "", fmt.Errorf("unsupported share item")
		}
		if f == nil {
			return "", fmt.Errorf("%s %d has no file", strings.ToLower(kind), id)
		}
		fp, err := Fingerprint(f)
		if err != nil {
			return "", fmt.Errorf("%s %d file is unavailable", strings.ToLower(kind), id)
		}
		item := models.ShareMedia{Key: key, Kind: kind, EntityID: id, FileID: f.Base().ID, Fingerprint: fp, Title: title}
		switch f := f.(type) {
		case *models.VideoFile:
			item.Width, item.Height, item.Duration, item.FrameRate, item.VideoCodec, item.AudioCodec = f.Width, f.Height, f.DurationFinite(), f.FrameRateFinite(), f.VideoCodec, f.AudioCodec
		case *models.ImageFile:
			item.Width, item.Height = f.Width, f.Height
		default:
			return "", fmt.Errorf("%s %d is not visual media", strings.ToLower(kind), id)
		}
		seen[key] = true
		ret.Media = append(ret.Media, item)
		return key, nil
	}
	seenEntries := make(map[Target]bool)
	for _, target := range targets {
		if target.ID <= 0 {
			return nil, fmt.Errorf("invalid share item")
		}
		if seenEntries[target] {
			continue
		}
		seenEntries[target] = true
		entry := models.ShareEntry{Kind: target.Kind, ID: target.ID, MediaKeys: []string{}}
		if target.Kind == "GALLERY" {
			g, err := s.Repo.Gallery.Find(ctx, target.ID)
			if err != nil {
				return nil, err
			}
			if g == nil {
				return nil, fmt.Errorf("gallery %d is unavailable", target.ID)
			}
			entry.Title = g.Title
			images, err := s.Repo.Image.FindByGalleryID(ctx, target.ID)
			if err != nil {
				return nil, err
			}
			if len(images) == 0 {
				return nil, fmt.Errorf("gallery %d has no images", target.ID)
			}
			for _, image := range images {
				key, err := add("IMAGE", image.ID)
				if err != nil {
					return nil, err
				}
				entry.MediaKeys = append(entry.MediaKeys, key)
			}
		} else {
			key, err := add(target.Kind, target.ID)
			if err != nil {
				return nil, err
			}
			entry.MediaKeys = append(entry.MediaKeys, key)
			for _, item := range ret.Media {
				if item.Key == key {
					entry.Title = item.Title
					break
				}
			}
		}
		ret.Entries = append(ret.Entries, entry)
	}
	return ret, nil
}

// Fingerprint also pins an archive's size and mtime for images inside ZIPs.
// A changed archive invalidates its old grants until the owner shares again.
func Fingerprint(f models.File) (models.SceneCoverFingerprint, error) {
	b := f.Base()
	source := b
	if b.ZipFileID != nil {
		if b.ZipFile == nil {
			return models.SceneCoverFingerprint{}, ErrUnavailable
		}
		source = b.ZipFile.Base()
	}
	stat, err := os.Stat(source.Path)
	if err != nil {
		return models.SceneCoverFingerprint{}, err
	}
	if !stat.Mode().IsRegular() {
		return models.SceneCoverFingerprint{}, ErrUnavailable
	}
	return models.SceneCoverFingerprint{Version: 1, Size: stat.Size(), ModTimeNano: stat.ModTime().UnixNano(), MD5: b.Fingerprints.GetString(models.FingerprintTypeMD5), OSHash: b.Fingerprints.GetString(models.FingerprintTypeOshash)}, nil
}

// Resolve only returns the current file when both the object relationship and
// the pinned file identity still match. IDs alone never authorize a request.
func (s *Service) Resolve(ctx context.Context, item models.ShareMedia) (models.File, *models.Scene, error) {
	var f models.File
	var scene *models.Scene
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		switch item.Kind {
		case "SCENE":
			var err error
			scene, err = s.Repo.Scene.Find(ctx, item.EntityID)
			if err != nil {
				return err
			}
			if scene == nil {
				return ErrUnavailable
			}
			if err := scene.LoadPrimaryFile(ctx, s.Repo.File); err != nil {
				return err
			}
			if scene.Files.Primary() != nil {
				f = scene.Files.Primary()
			}
		case "IMAGE":
			image, err := s.Repo.Image.Find(ctx, item.EntityID)
			if err != nil {
				return err
			}
			if image == nil {
				return ErrUnavailable
			}
			if err := image.LoadPrimaryFile(ctx, s.Repo.File); err != nil {
				return err
			}
			f = image.Files.Primary()
		default:
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if f == nil || f.Base().ID != item.FileID {
		return nil, nil, ErrUnavailable
	}
	current, err := Fingerprint(f)
	if err != nil {
		return nil, nil, ErrUnavailable
	}
	saved := item.Fingerprint
	if saved.Version != 1 || saved.Size != current.Size || saved.ModTimeNano != current.ModTimeNano || (saved.MD5 != "" && saved.MD5 != current.MD5) || (saved.OSHash != "" && saved.OSHash != current.OSHash) {
		return nil, nil, ErrUnavailable
	}
	return f, scene, nil
}
