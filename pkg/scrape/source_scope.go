package scrape

import (
	"net/url"
	"strings"

	"github.com/stashapp/stash/pkg/models"
	"golang.org/x/net/idna"
)

// SourceScopeV1 identifies the service actually contacted, independently of
// performer identity, account aliases, collection ownership and worker policy.
// Version this function when changing equivalence rules: persisted bindings and
// historical cooldowns must not silently acquire a different interpretation.
func SourceScopeV1(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || len(raw) > 8192 {
		return "", models.ErrSourceDefinitionConflict
	}
	host, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.ToLower(u.Hostname()), "."))
	if err != nil || host == "" || len(host) > 253 || strings.ContainsAny(host, "\r\n\x00") {
		return "", models.ErrSourceDefinitionConflict
	}
	host = strings.TrimPrefix(host, "www.")
	for _, service := range []struct {
		scope string
		hosts []string
	}{
		{"service:twitter", []string{"twitter.com", "x.com"}},
		{"service:reddit", []string{"reddit.com", "redd.it"}},
		{"service:bluesky", []string{"bsky.app", "bsky.social"}},
		{"service:tiktok", []string{"tiktok.com"}},
		{"service:instagram", []string{"instagram.com"}},
		{"service:patreon", []string{"patreon.com"}},
		{"service:onlyfans", []string{"onlyfans.com"}},
		{"service:fansly", []string{"fansly.com"}},
		{"service:imgur", []string{"imgur.com"}},
		{"service:redgifs", []string{"redgifs.com"}},
		{"mirror:coomer", []string{"coomer.st", "coomer.su", "coomer.party"}},
		{"mirror:kemono", []string{"kemono.cr", "kemono.su", "kemono.party"}},
	} {
		for _, domain := range service.hosts {
			if host == domain || strings.HasSuffix(host, "."+domain) {
				return service.scope, nil
			}
		}
	}
	// Unfamiliar gallery-dl services remain separate by host. No public-suffix
	// guess should combine unrelated sites on the same hosting provider.
	return "host:" + host, nil
}
