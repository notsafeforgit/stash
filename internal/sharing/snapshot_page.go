package sharing

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

type snapshotCursor struct {
	Share   string `json:"share"`
	Gallery string `json:"gallery,omitempty"`
	Entries []int  `json:"entries"`
	Media   []int  `json:"media"`
}

func (s *Service) snapshotPage(ctx context.Context, row *models.ShareRecord, cursor string, focus []string) (models.ShareSnapshot, string, error) {
	ret := models.ShareSnapshot{Entries: []models.ShareEntry{}, Media: []models.ShareMedia{}}
	if !Ready(row, time.Now()) {
		return ret, "", ErrUnavailable
	}
	gallery, media := "", ""
	if len(focus) > 0 {
		gallery = focus[0]
	}
	if len(focus) > 1 {
		media = focus[1]
	}
	position := snapshotCursor{Share: row.ID, Gallery: gallery, Entries: []int{0, 0, 0}, Media: []int{0, 0}}
	if cursor != "" {
		body, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(body) > 256 {
			return ret, "", errors.New("invalid share cursor")
		}
		if err = json.Unmarshal(body, &position); err != nil {
			return ret, "", err
		}
		if position.Share != row.ID || position.Gallery != gallery || len(position.Entries) != 3 || len(position.Media) != 2 {
			return ret, "", errors.New("invalid share cursor")
		}
	}
	for _, after := range append(append([]int{}, position.Entries...), position.Media...) {
		if after < -1 {
			return ret, "", errors.New("invalid share cursor")
		}
	}
	if gallery != "" {
		entries, err := s.Repo.Share.SnapshotEntries(ctx, row.ID, 0, 1, gallery, "GALLERY")
		if err != nil {
			return ret, "", err
		}
		if len(entries) != 1 {
			return ret, "", nil
		}
		ret.Entries = entries
		position.Entries = []int{-1, -1, -1}
		position.Media[1] = -1
	} else {
		// Independent type cursors keep images and galleries reachable even
		// when a selection begins with thousands of videos.
		for i, kind := range []string{"SCENE", "IMAGE", "GALLERY"} {
			if position.Entries[i] < 0 {
				continue
			}
			entries, err := s.Repo.Share.SnapshotEntries(ctx, row.ID, position.Entries[i], PageSize+1, "", kind)
			if err != nil {
				return ret, "", err
			}
			if len(entries) > PageSize {
				entries = entries[:PageSize]
				position.Entries[i] = entries[len(entries)-1].Position
			} else {
				position.Entries[i] = -1
			}
			ret.Entries = append(ret.Entries, entries...)
		}
		// The first page of a small selection still preserves its original
		// selected-entry order across kinds.
		slices.SortFunc(ret.Entries, func(a, b models.ShareEntry) int { return cmp.Compare(a.Position, b.Position) })
	}
	seen := map[string]bool{}
	add := func(item models.ShareMedia) error {
		if seen[item.Key] {
			return nil
		}
		_, _, err := s.resolve(ctx, item)
		if errors.Is(err, ErrUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		seen[item.Key] = true
		ret.Media = append(ret.Media, item)
		return nil
	}
	for i, kind := range []string{"SCENE", "IMAGE"} {
		if position.Media[i] < 0 {
			continue
		}
		items, err := s.Repo.Share.SnapshotMedia(ctx, row.ID, position.Media[i], PageSize+1, gallery, kind)
		if err != nil {
			return ret, "", err
		}
		if len(items) > PageSize {
			items = items[:PageSize]
			position.Media[i] = items[len(items)-1].Position
		} else {
			position.Media[i] = -1
		}
		for _, candidate := range items {
			var item models.ShareMedia
			if err := json.Unmarshal(candidate.MediaJSON, &item); err != nil {
				return ret, "", err
			}
			if err := add(item); err != nil {
				return ret, "", err
			}
		}
	}
	if media != "" {
		item, err := s.Repo.Share.SnapshotItem(ctx, row.ID, media)
		if err != nil {
			return ret, "", err
		}
		if item != nil {
			if err := add(*item); err != nil {
				return ret, "", err
			}
		}
	}
	// One thumbnail per gallery keeps overview work bounded even when a gallery
	// contains tens of thousands of files. Its full contents have their own cursor.
	if gallery == "" {
		for _, entry := range ret.Entries {
			if entry.Kind != "GALLERY" {
				continue
			}
			items, err := s.Repo.Share.SnapshotMedia(ctx, row.ID, 0, 1, entry.Key, "")
			if err != nil {
				return ret, "", err
			}
			for _, candidate := range items {
				var item models.ShareMedia
				if err := json.Unmarshal(candidate.MediaJSON, &item); err != nil {
					return ret, "", err
				}
				if err := add(item); err != nil {
					return ret, "", err
				}
			}
		}
	}
	keys := make([]string, 0, len(ret.Media))
	for _, item := range ret.Media {
		keys = append(keys, item.Key)
	}
	for i := range ret.Entries {
		var err error
		ret.Entries[i].MediaKeys, err = s.Repo.Share.SnapshotEntryKeys(ctx, row.ID, ret.Entries[i].Key, keys)
		if err != nil {
			return ret, "", err
		}
	}
	if slices.ContainsFunc(position.Media, func(v int) bool { return v >= 0 }) || slices.ContainsFunc(position.Entries, func(v int) bool { return v >= 0 }) {
		body, err := json.Marshal(position)
		return ret, base64.RawURLEncoding.EncodeToString(body), err
	}
	return ret, "", nil
}
