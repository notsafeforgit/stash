package scrape

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/stashapp/stash/pkg/models"
	"golang.org/x/net/idna"
)

// SourceOriginV1 retains the HTTP origin of an observed source dependency.
// It deliberately excludes paths, queries and fragments, which can contain
// signed access tokens. SourceScopeV1 continues to define pacing equivalence.
func SourceOriginV1(raw string) (string, error) {
	if _, err := SourceScopeV1(raw); err != nil || strings.TrimSpace(raw) != raw {
		return "", models.ErrSourceDefinitionConflict
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", models.ErrSourceDefinitionConflict
	}
	host, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.ToLower(u.Hostname()), "."))
	if err != nil || host == "" {
		return "", models.ErrSourceDefinitionConflict
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", models.ErrSourceDefinitionConflict
		}
		port = strconv.Itoa(n)
		if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
			port = ""
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return "", models.ErrSourceDefinitionConflict
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return (&url.URL{Scheme: u.Scheme, Host: host, Path: "/"}).String(), nil
}
