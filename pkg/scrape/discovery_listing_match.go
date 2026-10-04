package scrape

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const DiscoveryListingMatchPolicy = "retained-discovery-listing-v1"

// DiscoveryListingMatch is a candidate, never an accepted identity. Even a
// corroborated candidate must remain subject to completed enumeration, competing
// candidates, current native choices and atomic publication. NeedsDetail cannot
// be cleared merely because a later fetch confirms the same account or post ID.
type DiscoveryListingMatch struct {
	Policy      string                      `json:"policy"`
	Post        models.SourcePostIdentifier `json:"post"`
	URL         string                      `json:"url"`
	Basis       string                      `json:"basis"`
	NeedsDetail bool                        `json:"needs_detail"`
}

// RecordOrdinals refer to the original compact page, avoiding another copy of
// its post/profile/media payloads. Several attachments of one post are one
// candidate; different post IDs remain distinct even when their captions match.
type DiscoveryPageCandidate struct {
	DiscoveryListingMatch
	RecordOrdinals []int `json:"record_ordinals"`
}

type discoveryListingEvidence struct {
	platform string
	account  models.AccountReference
	titles   map[string]bool
	texts    map[string]bool
	dates    map[string]bool
	posts    map[models.SourcePostIdentifier]bool
}

func prepareListingEvidence(values map[string]any) (*discoveryListingEvidence, error) {
	prepared, reason := prepareDiscoveryTarget(values)
	if reason != "" || prepared.Status != "pending" {
		return nil, models.ErrDiscoveryInvalid
	}
	evidence, _, valid := discoveryObject(values["evidence_json"], CatalogChunkLimit)
	parts := strings.SplitN(prepared.AccountKey, ":", 3)
	if !valid || len(parts) != 3 || parts[0] != prepared.Platform || (parts[1] != "id" && parts[1] != "handle") {
		return nil, models.ErrDiscoveryInvalid
	}
	account, err := archive.NormalizeAccountReference(models.AccountReference{Namespace: "native:" + prepared.Platform, Kind: parts[1], Value: parts[2]})
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	set := func(key string) map[string]bool {
		ret := map[string]bool{}
		for _, value := range evidence[key].([]any) {
			ret[value.(string)] = true
		}
		return ret
	}
	posts := map[models.SourcePostIdentifier]bool{}
	for _, rawURL := range evidence["urls"].([]any) {
		// Only original, qualified post URLs count. Media/feed URLs and the
		// inferred detail-fetch URL cannot establish identity.
		if ref := discoveryURLIdentity(rawURL.(string), prepared.Platform); ref != nil {
			posts[*ref] = true
		}
	}
	return &discoveryListingEvidence{platform: prepared.Platform, account: account,
		titles: set("titles"), texts: set("texts"), dates: set("dates"), posts: posts}, nil
}

// MatchDiscoveryListing compares actual post metadata with the original held
// target. It intentionally does not use lookup-only filename or account shortcuts.
// Calling it again for a weak candidate's detail response uses unchanged target
// evidence, rather than turning the inferred URL into new corroboration.
func MatchDiscoveryListing(values map[string]any, raw json.RawMessage) (*DiscoveryListingMatch, error) {
	evidence, err := prepareListingEvidence(values)
	if err != nil {
		return nil, err
	}
	return evidence.match(raw)
}

func (e *discoveryListingEvidence) match(raw json.RawMessage) (*DiscoveryListingMatch, error) {
	post, err := archive.ExtractCapturedPost(raw)
	if err != nil || post == nil || post.Namespace != "native:"+e.platform {
		return nil, err
	}
	account, err := archive.ExtractCapturedAccount(raw)
	if err != nil {
		return nil, err
	}
	if account != nil && e.account.Kind == "id" {
		for _, id := range account.Identifiers {
			if id.Reference.Namespace == e.account.Namespace && id.Reference.Kind == "id" && id.Reference != e.account {
				return nil, nil // a contradictory publisher ID cannot be rescued by text
			}
		}
	}
	metadata, err := archive.CapturedMetadata(raw)
	if err != nil {
		return nil, err
	}
	words := func(value *string) string {
		if value == nil {
			return ""
		}
		return discoveryWords(*value)
	}
	title, text := words(metadata.Title), words(metadata.OriginalText)
	day := discoverySourceDay(metadata.PublishedAt)
	sameDay := day != "" && e.dates[day]
	sameTitle := utf8.RuneCountInString(title) >= 20 && e.titles[title]
	sameText := utf8.RuneCountInString(text) >= 40 && e.texts[text]
	result := &DiscoveryListingMatch{Policy: DiscoveryListingMatchPolicy, Post: *post}
	switch {
	case sameDay && sameTitle:
		result.Basis = "exact-title-and-date"
	case sameDay && sameText:
		result.Basis = "exact-original-text-and-date"
	case sameTitle && sameText && utf8.RuneCountInString(text) >= 64 && title != text:
		result.Basis = "exact-title-and-original-text"
	case sameDay && e.posts[*post]:
		result.Basis = "exact-source-url-and-date"
	default:
		if !sameTitle {
			return nil, nil
		}
		result.Basis, result.NeedsDetail = "title-needs-verification", true
	}
	result.URL = "https://www.reddit.com/comments/" + post.Value
	if e.platform == "twitter" {
		result.URL = "https://x.com/i/web/status/" + post.Value
	}
	if ref := discoveryURLIdentity(result.URL, e.platform); ref == nil || *ref != *post {
		return nil, models.ErrDiscoveryInvalid
	}
	return result, nil
}

// MatchDiscoveryPage produces bounded candidates for one retained page. It
// neither treats the page's completion flag as whole-import success nor chooses
// a unique winner across other pages. No native source or selected field changes.
func MatchDiscoveryPage(values map[string]any, raw json.RawMessage) ([]DiscoveryPageCandidate, error) {
	evidence, err := prepareListingEvidence(values)
	if err != nil {
		return nil, err
	}
	page, err := archive.ParseDiscoveryPage(raw)
	if err != nil {
		return nil, err
	}
	platform, err := archive.DiscoveryProfilePlatform(page.URL)
	if err != nil || platform != evidence.platform {
		return nil, models.ErrDiscoveryInvalid
	}
	ret := []DiscoveryPageCandidate{}
	positions := map[models.SourcePostIdentifier]int{}
	for ordinal := range page.Records {
		metadata, err := page.Metadata(ordinal)
		if err != nil {
			return nil, err
		}
		candidate, err := evidence.match(metadata)
		if err != nil {
			return nil, err
		}
		if candidate == nil {
			continue
		}
		index, found := positions[candidate.Post]
		if !found {
			index, positions[candidate.Post] = len(ret), len(ret)
			ret = append(ret, DiscoveryPageCandidate{DiscoveryListingMatch: *candidate, RecordOrdinals: []int{}})
		} else if ret[index].NeedsDetail && !candidate.NeedsDetail {
			ret[index].DiscoveryListingMatch = *candidate
		}
		ret[index].RecordOrdinals = append(ret[index].RecordOrdinals, ordinal)
	}
	return ret, nil
}
