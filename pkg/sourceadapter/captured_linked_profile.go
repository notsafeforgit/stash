package sourceadapter

import (
	"html"
	"strings"

	"github.com/stashapp/stash/pkg/jsonutil"
)

// CapturedLinkedProfile is a discovery link, not an account ownership or
// depicted-performer claim. The caller must match the source profile to the
// collection being collected before following it.
type CapturedLinkedProfile struct {
	SourceProfileURL string
	URL              string
	// Producer-observed destination, separate from website identity evidence.
	// Core validates this against the capture's root and collection scope.
	Directory string
}

// ExtractCapturedRedditRedgifsProfile uses the actual API item's uploader.
// Never guess a Redgifs handle from a Reddit username or a clip's URL slug.
func ExtractCapturedRedditRedgifsProfile(raw []byte) (*CapturedLinkedProfile, error) {
	data, err := jsonutil.DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	if data["category"] != "redgifs" {
		return nil, nil
	}
	post, _, category, err := capturedSourceContext(data)
	if err != nil || category != "reddit" {
		return nil, err
	}
	if sourceTruthy(post["comment"]) || sourceTruthy(post["crosspost_parent"]) || sourceTruthy(post["crosspost_parent_list"]) {
		return nil, nil
	}
	identity, err := ExtractCapturedAccount(raw)
	if err != nil || identity == nil {
		return nil, err
	}
	author := ""
	for _, claim := range identity.Identifiers {
		if claim.Reference.Namespace == "native:reddit" && claim.Reference.Kind == "handle" {
			author = claim.Reference.Value
		}
	}
	if author == "" {
		return nil, nil
	}
	item, err := capturedRedgifsPost(data, "")
	if err != nil || item == nil {
		return nil, err
	}
	linked := false
	requested, _, _ := redditExternalReference(data["source_extractor_url"])
	matches := func(value interface{}) bool {
		ref, _, _ := redditExternalReference(value)
		if ref == nil || ref.Namespace != "native:redgifs" {
			return false
		}
		// Some Redgifs links use an old numeric alias. Only the captured
		// extractor request can establish that it resolved to this API item.
		return *ref == *item || capturedNumericPost.MatchString(ref.Value) && requested != nil && *requested == *ref
	}
	for _, key := range []string{"url", "url_overridden_by_dest"} {
		if ref, _, _ := redditExternalReference(post[key]); ref != nil && ref.Namespace == "native:redgifs" && !matches(post[key]) {
			return nil, nil
		}
		linked = linked || matches(post[key])
	}
	if body, ok := post["selftext"].(string); ok && !linked {
		for _, value := range bioURLPattern.FindAllString(html.UnescapeString(body), 128) {
			linked = linked || matches(strings.TrimRight(value, ".,;!?)]}"))
		}
	}
	uploader, ok := data["userName"].(string)
	if !linked || !ok || !profilePatterns["redgifs"].MatchString("/users/"+uploader) {
		return nil, nil
	}
	directory, _ := data["native_redgifs_directory"].(string)
	return &CapturedLinkedProfile{
		SourceProfileURL: "https://www.reddit.com/user/" + author + "/",
		URL:              "https://www.redgifs.com/users/" + strings.ToLower(uploader),
		Directory:        directory,
	}, nil
}
