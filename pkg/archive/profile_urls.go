package archive

import (
	"html"
	"regexp"
	"slices"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

// ProfileURLKey compares known account locators by service identity, including
// x.com/twitter.com aliases. Other websites keep their path and query spelling.
// This is an offline comparison; it never resolves redirects or claims ownership.
func ProfileURLKey(value string) string {
	if len(value) > 4096 {
		return ""
	}
	canonical, ok := CanonicalProfileURL(value)
	if !ok || len(canonical) > 4096 {
		return ""
	}
	if ref := ProfileReference(canonical); ref != nil {
		return ref.Namespace + ":" + ref.Kind + ":" + ref.Value
	}
	return "url:" + canonical
}

var bioURLPattern = regexp.MustCompile("(?i)https?://[^\\s<>\"'`]+")

// ExtractCapturedProfileURLs reads only account profile fields already present
// in the capture. In particular, user can be a feed owner rather than the post
// author. A matching stable account ID is required before inspecting its bio.
// Post text, artwork, media URLs and nested quoted/reposted users are excluded.
func ExtractCapturedProfileURLs(raw []byte) ([]string, error) {
	identity, err := ExtractCapturedAccount(raw)
	if err != nil || identity == nil {
		return nil, err
	}
	data, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	data, _, category, err := capturedSourceContext(data)
	if err != nil {
		return nil, err
	}
	links := map[string]string{}
	for _, role := range []string{"author", "user", "owner", "uploader", "creator", "account", "blog", "user_profile"} {
		profile, ok := data[role].(sourceObject)
		if !ok || !profileMatchesPublisher(profile, identity, category, data) {
			continue
		}
		collectProfileURLs(profile, links)
		if category == "reddit" {
			if page, ok := profile["subreddit"].(sourceObject); ok {
				collectProfileURLs(page, links)
			}
		}
		if category == "twitter" {
			if legacy, ok := profile["legacy"].(sourceObject); ok {
				collectProfileURLs(legacy, links)
			}
		}
	}
	keys := make([]string, 0, len(links))
	for key := range links {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	ret := make([]string, 0, len(keys))
	for _, key := range keys {
		ret = append(ret, links[key])
	}
	return ret, nil
}

func profileMatchesPublisher(profile sourceObject, identity *CapturedAccount, category string, data sourceObject) bool {
	kind := "id"
	keys := []string{"id", "did", "uuid"}
	switch category {
	case "twitter":
		keys = []string{"rest_id", "id_str", "id"}
	case "instagram":
		keys = []string{"pk", "id"}
	case "bluesky":
		keys = []string{"did"}
	case "tumblr":
		keys = []string{"uuid"}
	case "coomer", "kemono":
		if profile["service"] != data["service"] {
			return false
		}
		kind = "user"
	case "tiktok":
		if !sourceTruthy(profile["id"]) {
			keys, kind = []string{"secUid"}, "secUid"
		}
	}
	value, err := capturedIdentifierValue(firstCapturedField(profile, "", keys...))
	if err != nil || value == "" {
		return false
	}
	if category == "reddit" && !strings.HasPrefix(value, "t2_") {
		value = "t2_" + value
	}
	ref, err := NormalizeAccountReference(models.AccountReference{Namespace: identity.Namespace, Kind: kind, Value: value})
	if err != nil {
		return false
	}
	for _, claim := range identity.Identifiers {
		if claim.Reference == ref {
			return true
		}
	}
	return false
}

func collectProfileURLs(profile sourceObject, links map[string]string) {
	// Twitter supplies expanded URLs in entities; do not follow short links or
	// retain both the short and expanded spelling when the latter was captured.
	expansions := map[string]string{}
	if entities, ok := profile["entities"].(sourceObject); ok {
		for _, field := range []string{"url", "description"} {
			section, _ := entities[field].(sourceObject)
			items, _ := section["urls"].([]interface{})
			for _, item := range items {
				entry, _ := item.(sourceObject)
				short, _ := entry["url"].(string)
				expanded, _ := entry["expanded_url"].(string)
				if short != "" && ProfileURLKey(expanded) != "" {
					expansions[short] = expanded
				}
			}
		}
	}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if expanded := expansions[value]; expanded != "" {
			value = expanded
		}
		key := ProfileURLKey(value)
		if key != "" && len(links) < 128 {
			if _, exists := links[key]; !exists {
				links[key] = value
			}
		}
	}
	for _, field := range []string{"url", "website", "website_url", "external_url"} {
		if value, ok := profile[field].(string); ok {
			add(value)
		}
	}
	for _, field := range []string{"description", "bio", "biography", "signature", "about", "public_description"} {
		if value, ok := profile[field].(string); ok {
			for _, link := range bioURLPattern.FindAllString(html.UnescapeString(value), -1) {
				link = strings.TrimRight(link, ".,;!?")
				for _, pair := range [][2]string{{"(", ")"}, {"[", "]"}, {"{", "}"}} {
					for strings.HasSuffix(link, pair[1]) && strings.Count(link, pair[1]) > strings.Count(link, pair[0]) {
						link = strings.TrimSuffix(link, pair[1])
					}
				}
				add(link)
			}
		}
	}
	// Named profile link lists only; never recursively harvest arbitrary URLs.
	for _, field := range []string{"bio_links", "bioLink", "external_urls", "links", "social_links"} {
		items, ok := profile[field].([]interface{})
		if !ok {
			items = []interface{}{profile[field]}
		}
		for _, item := range items {
			switch item := item.(type) {
			case string:
				add(item)
			case sourceObject:
				for _, key := range []string{"url", "link"} {
					if value, ok := item[key].(string); ok {
						add(value)
					}
				}
			}
		}
	}
	// Entity entries can contain a website absent from the display bio.
	shorts := make([]string, 0, len(expansions))
	for short := range expansions {
		shorts = append(shorts, short)
	}
	slices.Sort(shorts)
	for _, short := range shorts {
		add(short)
	}
}
