// Package archive owns source identity and provenance rules for the native
// library. Source accounts and depicted performers are distinct concepts.
package archive

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/stashapp/stash/pkg/models"
)

var profilePatterns = map[string]*regexp.Regexp{
	"twitter_id":     regexp.MustCompile(`^/i/user/([0-9]+)$`),
	"twitter":        regexp.MustCompile(`^/([A-Za-z0-9_]{1,15})(?:/(?:media|with_replies))?$`),
	"reddit":         regexp.MustCompile(`^/(?:user|u)/([A-Za-z0-9_-]{1,32})$`),
	"instagram":      regexp.MustCompile(`^/([A-Za-z0-9_.]{1,30})$`),
	"bluesky":        regexp.MustCompile(`^/profile/([^/?#%]+)(?:/(?:posts|media|replies|video))?$`),
	"bluesky_id":     regexp.MustCompile(`^did:(?:plc:[a-zA-Z0-9]+|web:[A-Za-z0-9.:-]+)$`),
	"bluesky_handle": regexp.MustCompile(`^[A-Za-z0-9.-]+\.[A-Za-z0-9-]+$`),
	"tiktok":         regexp.MustCompile(`^/@([A-Za-z0-9_.-]+)(?:/(?:posts|reposts|stories))?$`),
	"mirror_host":    regexp.MustCompile(`^(?:beta\.)?(coomer|kemono)\.(?:st|su|cr|party)$`),
	"mirror":         regexp.MustCompile(`^/([A-Za-z0-9_-]+)/user/([^/?#%]+)$`),
	"coomerfans":     regexp.MustCompile(`^/u/([A-Za-z0-9_-]+)/([0-9]+)/[^/?#%]+$`),
	"subscription":   regexp.MustCompile(`^/([A-Za-z0-9_.-]+)$`),
	"patreon":        regexp.MustCompile(`^/(?:c/)?([A-Za-z0-9_.-]+)$`),
	"patreon_id":     regexp.MustCompile(`^u=([0-9]+)$`),
	"tumblr":         regexp.MustCompile(`^/(?:blog/)?([A-Za-z0-9-]+)$`),
}

// CanonicalProfileURL is an offline locator normalization, not evidence that a
// URL belongs to a person. Unknown services retain path spelling and query data.
// Credentials, explicit ports, control characters and non-HTTP schemes are not
// accepted as profile identifiers. No redirects or remote requests are followed.
func CanonicalProfileURL(value string) (string, bool) {
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", false
	}
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Port() != "" || strings.HasSuffix(u.Host, ":") || u.Opaque != "" {
		return "", false
	}
	u.Scheme = "https"
	u.Host = strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	// Trim literal separators without decoding an escaped slash that may be
	// part of an opaque account identifier on an unknown service.
	u.RawPath = strings.TrimRight(u.EscapedPath(), "/")
	u.Path, _ = url.PathUnescape(u.RawPath)
	u.Fragment, u.RawFragment = "", ""
	return u.String(), true
}

func nativeProfileHandle(platform, value string, excluded string) *models.AccountReference {
	value = strings.ToLower(value)
	for _, reserved := range strings.Fields(excluded) {
		if value == reserved {
			return nil
		}
	}
	return &models.AccountReference{Namespace: "native:" + platform, Kind: "handle", Value: value}
}

// ProfileReference recognizes explicit account URLs. A post, feed, or search
// URL is not promoted into an author identifier. Native/mirror scopes remain
// separate even when their opaque values happen to have the same spelling.
func ProfileReference(value string) *models.AccountReference {
	canonical, valid := CanonicalProfileURL(value)
	if !valid {
		return nil
	}
	u, _ := url.Parse(canonical)
	host, path := u.Hostname(), u.EscapedPath()
	match := func(pattern string) []string { return profilePatterns[pattern].FindStringSubmatch(path) }
	switch host {
	case "mobile.twitter.com", "mobile.x.com", "twitter.com", "x.com":
		if m := match("twitter_id"); m != nil {
			return &models.AccountReference{Namespace: "native:twitter", Kind: "id", Value: m[1]}
		}
		if m := match("twitter"); m != nil {
			return nativeProfileHandle("twitter", m[1], "home search explore settings messages notifications intent share login logout signup i")
		}
	case "reddit.com", "old.reddit.com", "new.reddit.com":
		if m := match("reddit"); m != nil {
			return nativeProfileHandle("reddit", m[1], "")
		}
	case "instagram.com":
		if m := match("instagram"); m != nil {
			return nativeProfileHandle("instagram", m[1], "p reel reels stories explore accounts direct about developer legal web")
		}
	case "bsky.app", "main.bsky.dev":
		if m := match("bluesky"); m != nil {
			if profilePatterns["bluesky_id"].MatchString(m[1]) {
				return &models.AccountReference{Namespace: "native:bluesky", Kind: "id", Value: m[1]}
			}
			if profilePatterns["bluesky_handle"].MatchString(m[1]) {
				return nativeProfileHandle("bluesky", m[1], "")
			}
		}
	case "tiktok.com", "tiktokv.com", "m.tiktok.com":
		if m := match("tiktok"); m != nil {
			return nativeProfileHandle("tiktok", m[1], "")
		}
	case "onlyfans.com", "fansly.com":
		if m := match("subscription"); m != nil {
			return nativeProfileHandle(strings.SplitN(host, ".", 2)[0], m[1], "home explore search my login signup terms privacy")
		}
	case "patreon.com":
		if path == "/user" {
			if m := profilePatterns["patreon_id"].FindStringSubmatch(u.RawQuery); m != nil {
				return &models.AccountReference{Namespace: "native:patreon", Kind: "id", Value: m[1]}
			}
		} else if m := match("patreon"); m != nil {
			return nativeProfileHandle("patreon", m[1], "posts login signup home explore settings creation user")
		}
	case "tumblr.com":
		if m := match("tumblr"); m != nil {
			return nativeProfileHandle("tumblr", m[1], "dashboard explore search settings login register")
		}
	case "coomerfans.com":
		if m := match("coomerfans"); m != nil {
			return &models.AccountReference{Namespace: "mirror:coomer:" + strings.ToLower(m[1]), Kind: "user", Value: m[2]}
		}
	default:
		if h := profilePatterns["mirror_host"].FindStringSubmatch(host); h != nil {
			if m := match("mirror"); m != nil {
				return &models.AccountReference{Namespace: "mirror:" + h[1] + ":" + strings.ToLower(m[1]), Kind: "user", Value: m[2]}
			}
		}
		if strings.HasSuffix(host, ".tumblr.com") && strings.Count(host, ".") == 2 && path == "" {
			return nativeProfileHandle("tumblr", strings.SplitN(host, ".", 2)[0], "www assets static media")
		}
	}
	return nil
}

// ProfileReferences retains an exact URL locator for other gallery-dl services
// as well as any recognized typed identifier. The caller must establish that
// the URL was attached to the captured author or explicitly supplied by a user.
func ProfileReferences(value string) []models.AccountReference {
	canonical, valid := CanonicalProfileURL(value)
	if !valid {
		return nil
	}
	ret := []models.AccountReference{{Namespace: "url", Kind: "profile", Value: canonical}}
	if ref := ProfileReference(value); ref != nil {
		ret = append(ret, *ref)
	}
	return ret
}
