package scrape

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

var backfillAccount = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
var backfillTwitterID = regexp.MustCompile(`^[0-9]{1,30}$`)

func NormalizeBackfillSubject(subject models.BackfillSubject) (models.BackfillSubject, error) {
	id, err := uuid.Parse(subject.RootUUID)
	if err != nil || id == uuid.Nil || id.String() != subject.RootUUID {
		return subject, models.ErrBackfillInvalid
	}
	switch subject.Platform {
	case "reddit":
		if !backfillAccount.MatchString(subject.Account) || strings.EqualFold(subject.Account, "me") {
			return subject, models.ErrBackfillInvalid
		}
		subject.Account = strings.ToLower(subject.Account)
	case "twitter":
		if !backfillTwitterID.MatchString(subject.Account) {
			return subject, models.ErrBackfillInvalid
		}
		subject.Account = strings.TrimLeft(subject.Account, "0")
		if subject.Account == "" {
			subject.Account = "0"
		}
	default:
		return subject, models.ErrBackfillInvalid
	}
	return subject, nil
}

func RequiredBackfillComponents(platform string) []string {
	if platform == "twitter" {
		return []string{"twitter"}
	}
	if platform == "reddit" {
		return []string{"reddit-new", "reddit-top"}
	}
	return nil
}

// These are the existing n8n component definitions, including its separate
// profile-new URL's t=all. Keep exact URLs: normalization must not redirect an
// accepted backfill to a different account or traversal definition.
func BackfillTargets(subject models.BackfillSubject, component string) ([]string, error) {
	if _, err := NormalizeBackfillSubject(subject); err != nil {
		return nil, err
	}
	if subject.Platform == "twitter" {
		if component != "twitter" {
			return nil, models.ErrBackfillInvalid
		}
		return []string{"https://x.com/i/user/" + subject.Account}, nil
	}
	profile := "https://www.reddit.com/user/" + subject.Account + "/submitted/?sort="
	search := "https://www.reddit.com/search?q=author%3A" + subject.Account + "+nsfw%3Ayes&include_over_18=on&sort="
	switch component {
	case "reddit-new":
		return []string{profile + "new", search + "new&t=all"}, nil
	case "reddit-top":
		return []string{profile + "top&t=all", search + "top&t=all", profile + "top&t=year", search + "top&t=year"}, nil
	case "reddit-profile-new":
		return []string{profile + "new&t=all"}, nil
	case "reddit-profile-top-all":
		return []string{profile + "top&t=all"}, nil
	case "reddit-search-new":
		return []string{search + "new&t=all"}, nil
	case "reddit-search-top-all":
		return []string{search + "top&t=all"}, nil
	case "reddit-search-top-year":
		return []string{search + "top&t=year"}, nil
	}
	return nil, models.ErrBackfillInvalid
}
