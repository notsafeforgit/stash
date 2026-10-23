package sharing

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

type snapshotCursor struct {
	Share   string `json:"share"`
	Gallery string `json:"gallery,omitempty"`
	Entries int    `json:"entries"`
	Media   int    `json:"media"`
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
	position := snapshotCursor{Share: row.ID, Gallery: gallery}
	if cursor != "" {
		body, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(body) > 256 {
			return ret, "", errors.New("invalid share cursor")
		}
		if err = json.Unmarshal(body, &position); err != nil {
			return ret, "", err
		}
		if position.Share != row.ID || position.Gallery != gallery || position.Entries < -1 || position.Media < -1 {
			return ret, "", errors.New("invalid share cursor")
		}
	}
	if gallery != "" {
		entries, err := s.Repo.Share.SnapshotEntries(ctx, row.ID, 0, 1, gallery)
		if err != nil {
			return ret, "", err
		}
		if len(entries) != 1 || entries[0].Kind != "GALLERY" {
			return ret, "", ErrUnavailable
		}
		ret.Entries = entries
		position.Entries = -1
	} else if position.Entries >= 0 {
		entries, err := s.Repo.Share.SnapshotEntries(ctx, row.ID, position.Entries, PageSize+1, "")
		if err != nil {
			return ret, "", err
		}
		if len(entries) > PageSize {
			entries = entries[:PageSize]
			position.Entries = entries[len(entries)-1].Position
		} else {
			position.Entries = -1
		}
		ret.Entries = entries
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
	if position.Media >= 0 {
		items, err := s.Repo.Share.SnapshotMedia(ctx, row.ID, position.Media, PageSize+1, gallery)
		if err != nil {
			return ret, "", err
		}
		if len(items) > PageSize {
			items = items[:PageSize]
			position.Media = items[len(items)-1].Position
		} else {
			position.Media = -1
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
			items, err := s.Repo.Share.SnapshotMedia(ctx, row.ID, 0, 1, entry.Key)
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
	if position.Media >= 0 || position.Entries >= 0 {
		body, err := json.Marshal(position)
		return ret, base64.RawURLEncoding.EncodeToString(body), err
	}
	return ret, "", nil
}
