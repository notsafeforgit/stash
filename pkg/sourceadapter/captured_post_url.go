package sourceadapter

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/stashapp/stash/pkg/jsonutil"
	"github.com/stashapp/stash/pkg/models"
)

type CapturedPostURL struct {
	Post models.SourcePostIdentifier
	URL  string
}

var redditPostID = regexp.MustCompile(`^[a-z0-9]{1,32}$`)
var redditPermalink = regexp.MustCompile(`^/(?:(?:r|u|user)/[^/]+/)?comments/([a-z0-9]+)(?:/[^/]*)?/?$`)

// ExtractCapturedPostURL identifies the enclosing post, never its attachment,
// crosspost parent or discovery feed. Missing/invalid optional permalinks do
// not prevent ingestion: Reddit and Twitter have stable routes by post ID.
// Unsupported source formats return nil rather than guessing a URL.
func ExtractCapturedPostURL(raw []byte) (*CapturedPostURL, error) {
	post, err := ExtractCapturedPost(raw)
	if err != nil || post == nil {
		return nil, err
	}
	switch post.Namespace {
	case "native:erome":
		return &CapturedPostURL{Post: *post, URL: "https://www.erome.com/a/" + post.Value}, nil
	case "native:reddit":
		if !redditPostID.MatchString(post.Value) {
			return nil, nil
		}
		data, err := jsonutil.DecodeJSONObject(raw, MaxSourcePayloadBytes)
		if err != nil {
			return nil, err
		}
		data, _, _, err = capturedSourceContext(data)
		if err != nil {
			return nil, err
		}
		permalink, _ := data["permalink"].(string)
		if parsed, err := url.Parse(permalink); err == nil && len(permalink) <= 8192 && parsed.User == nil && parsed.Port() == "" {
			host := strings.ToLower(parsed.Hostname())
			local := parsed.Scheme == "" && parsed.Host == "" && strings.HasPrefix(permalink, "/") && !strings.HasPrefix(permalink, "//")
			remote := (parsed.Scheme == "https" || parsed.Scheme == "http") &&
				(host == "reddit.com" || host == "www.reddit.com" || host == "old.reddit.com" || host == "new.reddit.com" || host == "np.reddit.com")
			match := redditPermalink.FindStringSubmatch(parsed.EscapedPath())
			if (local || remote) && len(match) == 2 && match[1] == post.Value && len(parsed.EscapedPath())+len("https://www.reddit.com") <= 8192 {
				return &CapturedPostURL{Post: *post, URL: "https://www.reddit.com" + parsed.EscapedPath()}, nil
			}
		}
		return &CapturedPostURL{Post: *post, URL: "https://www.reddit.com/comments/" + post.Value}, nil
	case "native:twitter":
		if capturedNumericPost.MatchString(post.Value) {
			return &CapturedPostURL{Post: *post, URL: "https://x.com/i/web/status/" + post.Value}, nil
		}
	}
	return nil, nil
}
