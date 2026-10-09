package scrape

import (
	"net/url"
	"regexp"
	"strings"
)

var redditProfileHandle = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// RedditProfileURL recognizes only a profile and the standard retrieval passes
// used for that profile. Arbitrary searches, comments, saved feeds and subreddit
// listings remain independent sources.
func RedditProfileURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Port() != "" {
		return ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "reddit.com", "www.reddit.com", "old.reddit.com":
	default:
		return ""
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return ""
	}
	for _, values := range q {
		if len(values) != 1 {
			return ""
		}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	handle := ""
	switch {
	case (len(parts) == 2 || (len(parts) == 3 && parts[2] == "submitted")) && (parts[0] == "user" || parts[0] == "u"):
		handle = parts[1]
	case len(parts) == 1 && parts[0] == "search" && q.Get("include_over_18") == "on":
		query := q.Get("q")
		if !strings.HasPrefix(query, "author:") || !strings.HasSuffix(query, " nsfw:yes") {
			return ""
		}
		handle = strings.TrimSuffix(strings.TrimPrefix(query, "author:"), " nsfw:yes")
		q.Del("q")
		q.Del("include_over_18")
	default:
		return ""
	}
	if !redditProfileHandle.MatchString(handle) {
		return ""
	}
	sort, period := q.Get("sort"), q.Get("t")
	q.Del("sort")
	q.Del("t")
	if len(q) != 0 || (sort != "" && sort != "new" && sort != "top") ||
		(period != "" && period != "all" && (sort != "top" || period != "year")) {
		return ""
	}
	return "https://www.reddit.com/user/" + strings.ToLower(handle) + "/"
}

// RetrievalURL validates a requested pass against its subscribed profile.
// The empty value retains exact historical requests and a normal profile run.
func RetrievalURL(source, retrieval string) bool {
	if retrieval == "" {
		return true
	}
	return RedditProfileURL(source) == source && RedditProfileURL(retrieval) == source
}
