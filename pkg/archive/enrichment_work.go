package archive

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

const MaxEnrichmentCaptures = 1024

var enrichmentPaths = map[string]*regexp.Regexp{
	"reddit_short": regexp.MustCompile(`^/[A-Za-z0-9]+/?$`),
	"reddit":       regexp.MustCompile(`^/(?:(?:(?:r|u|user)/[^/]+/)?comments|gallery)/[A-Za-z0-9]+(?:/[^/]*)?/?$`),
	"twitter":      regexp.MustCompile(`^/(?:[^/]+|i/web)/status/[0-9]+(?:/(?:photo|video)/[0-9]+)?/?$`),
	"bluesky":      regexp.MustCompile(`^/profile/[^/]+/post/[A-Za-z0-9._~-]+/?$`),
	"tiktok":       regexp.MustCompile(`^/@[^/]+/(?:video|photo)/[0-9]+/?$`),
	"instagram":    regexp.MustCompile(`^/(?:p|reel|tv)/[A-Za-z0-9_-]+/?$`),
	"mirror":       regexp.MustCompile(`^/[A-Za-z0-9_-]+/user/[^/]+/post/[^/]+/?$`),
	"patreon":      regexp.MustCompile(`^/posts/(?:[^/]*-)?[0-9]+/?$`),
	"fansly":       regexp.MustCompile(`^/post/[0-9]+/?$`),
}

// EnrichmentPostURL admits direct post routes supported by the pinned metadata
// collector. It is a fetch boundary, not proof of the post's canonical identity.
func EnrichmentPostURL(raw string) bool {
	if len(raw) == 0 || len(raw) > 8192 || strings.IndexFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Port() != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	category := ""
	switch host {
	case "reddit.com", "old.reddit.com", "new.reddit.com", "np.reddit.com":
		category = "reddit"
	case "redd.it":
		category = "reddit_short"
	case "x.com", "twitter.com", "mobile.twitter.com", "mobile.x.com":
		category = "twitter"
	case "bsky.app":
		category = "bluesky"
	case "tiktok.com":
		category = "tiktok"
	case "instagram.com":
		category = "instagram"
	case "coomer.st", "coomer.su", "coomer.cr", "coomer.party", "kemono.cr", "kemono.st", "kemono.su", "kemono.party":
		category = "mirror"
	case "patreon.com":
		category = "patreon"
	case "fansly.com":
		category = "fansly"
	default:
		return false
	}
	return enrichmentPaths[category].MatchString(u.EscapedPath())
}

func EnrichmentTargetIdentity(input models.EnrichmentTargetInput) (string, error) {
	for _, value := range []string{input.PostUUID, input.URLUUID, input.CollectionUUID} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return "", models.ErrEnrichmentInvalid
		}
	}
	if input.CollectionRevision <= 0 || input.Policy != models.EnrichmentGalleryMetadataV1 || (input.Origin != "review" && input.Origin != "migration") {
		return "", models.ErrEnrichmentInvalid
	}
	body, err := EncodeSourceJSON([]any{"stash-enrichment-target-v1", input.PostUUID, input.URLUUID, input.CollectionUUID, input.CollectionRevision, input.Policy})
	if err != nil {
		return "", err
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, body).String(), nil
}

func PrepareEnrichmentCompletion(input models.EnrichmentCompletionInput) (models.EnrichmentCompletionInput, error) {
	if input.ExpectedRevision < 1 || len(input.CaptureUUIDs) == 0 || len(input.CaptureUUIDs) > MaxEnrichmentCaptures {
		return input, models.ErrEnrichmentInvalid
	}
	values := append([]string{input.UUID, input.TargetUUID}, input.CaptureUUIDs...)
	for _, value := range values {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return input, models.ErrEnrichmentInvalid
		}
	}
	input.CaptureUUIDs = append([]string(nil), input.CaptureUUIDs...)
	sort.Strings(input.CaptureUUIDs)
	for i := 1; i < len(input.CaptureUUIDs); i++ {
		if input.CaptureUUIDs[i] == input.CaptureUUIDs[i-1] {
			return input, models.ErrEnrichmentInvalid
		}
	}
	return input, nil
}
