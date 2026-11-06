package sourceadapter

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const TwitterDateBasis = "twitter-snowflake"

var twitterStatusPath = regexp.MustCompile(`^/(?:[A-Za-z0-9_]{1,15}|i/web)/status/([0-9]+)(?:/(?:photo|video)/[0-9]+)?/?$`)

// TwitterPostID accepts saved post permalinks, excluding account and media IDs.
func TwitterPostID(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Port() != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return ""
	}
	switch strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.") {
	case "twitter.com", "x.com", "mobile.twitter.com", "mobile.x.com":
	default:
		return ""
	}
	match := twitterStatusPath.FindStringSubmatch(parsed.EscapedPath())
	if len(match) != 2 || !TwitterThreadID(match[1]) {
		return ""
	}
	return match[1]
}

// TwitterPublicationDate decodes the integer timestamp used by gallery-dl and
// Twitter's Snowflake implementation. Early IDs predate the format transition;
// require a date in 2011 or later instead of guessing their publication time.
func TwitterPublicationDate(identity string) *string {
	if !TwitterThreadID(identity) {
		return nil
	}
	id, _ := strconv.ParseUint(identity, 10, 64)
	published := time.UnixMilli(int64(id>>22) + 1288834974657).UTC()
	if published.Year() < 2011 || published.After(time.Now().UTC()) {
		return nil
	}
	value := published.Format(time.RFC3339Nano)
	return &value
}
