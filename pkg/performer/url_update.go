package performer

import (
	"context"
	"fmt"
	"slices"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/utils"
)

// ApplyUpdateURLs applies the current API's single URL/Twitter/Instagram fields
// to a loaded URL collection, preserving unrelated URLs.
func ApplyUpdateURLs(ctx context.Context, repository models.PerformerReaderWriter, id int, changes URLUpdate, partial *models.PerformerPartial) error {
	p, err := repository.Find(ctx, id)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("performer with id %d not found", id)
	}
	if err := p.LoadURLs(ctx, repository); err != nil {
		return err
	}
	return ApplyURLUpdate(p, changes, partial)
}

func ApplyURLUpdate(p *models.Performer, legacyURLs URLUpdate, updatedPerformer *models.PerformerPartial) error {
	existingURLs := slices.Clone(p.URLs.List())

	// performer partial URLs should be empty
	if legacyURLs.URL.Set {
		replaced := false
		for i, url := range existingURLs {
			if !IsTwitterURL(url) && !IsInstagramURL(url) {
				existingURLs[i] = legacyURLs.URL.Value
				replaced = true
				break
			}
		}

		if !replaced {
			existingURLs = append(existingURLs, legacyURLs.URL.Value)
		}
	}

	if legacyURLs.Twitter.Set {
		value := utils.URLFromHandle(legacyURLs.Twitter.Value, "https://twitter.com")
		found := false
		// find and replace the first twitter URL
		for i, url := range existingURLs {
			if IsTwitterURL(url) {
				existingURLs[i] = value
				found = true
				break
			}
		}

		if !found {
			existingURLs = append(existingURLs, value)
		}
	}
	if legacyURLs.Instagram.Set {
		found := false
		value := utils.URLFromHandle(legacyURLs.Instagram.Value, "https://instagram.com")
		// find and replace the first instagram URL
		for i, url := range existingURLs {
			if IsInstagramURL(url) {
				existingURLs[i] = value
				found = true
				break
			}
		}

		if !found {
			existingURLs = append(existingURLs, value)
		}
	}

	updatedPerformer.URLs = &models.UpdateStrings{
		Values: existingURLs,
		Mode:   models.RelationshipUpdateModeSet,
	}

	return nil
}

type URLUpdate struct {
	URL       models.OptionalString
	Twitter   models.OptionalString
	Instagram models.OptionalString
}

func (u *URLUpdate) AnySet() bool {
	return u.URL.Set || u.Twitter.Set || u.Instagram.Set
}
