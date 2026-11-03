package scrape

import (
	"net/url"
	"regexp"
	"strings"
)

var redgifsProfilePath = regexp.MustCompile(`^/users/([A-Za-z0-9_-]{1,256})/?$`)

// RedgifsProfileURL identifies an account listing, never an individual clip.
func RedgifsProfileURL(value string) string {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "redgifs.com" && host != "www.redgifs.com" {
		return ""
	}
	m := redgifsProfilePath.FindStringSubmatch(u.EscapedPath())
	if m == nil {
		return ""
	}
	return "https://www.redgifs.com/users/" + strings.ToLower(m[1])
}
