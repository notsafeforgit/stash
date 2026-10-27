package sourceadapter

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/stashapp/stash/pkg/jsonutil"
	"github.com/stashapp/stash/pkg/models"
)

// AccountProfile interprets a profile observation independently of any post.
// A stable service ID and the profile's own handle must both be present.
type AccountProfile struct {
	ID     models.AccountReference
	Handle models.AccountReference
	URL    string
	URLs   []string
}

var redditProfileID = regexp.MustCompile(`^(?:t2_)?[a-z0-9]+$`)
var redditProfileHandle = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func ExtractAccountProfile(raw []byte) (*AccountProfile, error) {
	data, err := jsonutil.DecodeJSONObject(raw, 65536)
	if err != nil {
		return nil, err
	}
	user, ok := data["user"].(sourceObject)
	if len(data) != 2 || data["category"] != "reddit" || !ok {
		return nil, errors.New("unsupported account profile")
	}
	id, _ := user["id"].(string)
	handle, _ := user["name"].(string)
	if !redditProfileID.MatchString(id) || !redditProfileHandle.MatchString(handle) {
		return nil, errors.New("profile requires a stable ID and handle")
	}
	if !strings.HasPrefix(id, "t2_") {
		id = "t2_" + id
	}
	ret := &AccountProfile{}
	ret.ID, err = NormalizeAccountReference(models.AccountReference{Namespace: "native:reddit", Kind: "id", Value: id})
	if err != nil {
		return nil, err
	}
	ret.Handle, err = NormalizeAccountReference(models.AccountReference{Namespace: "native:reddit", Kind: "handle", Value: handle})
	if err != nil {
		return nil, err
	}
	ret.URL = "https://www.reddit.com/user/" + ret.Handle.Value
	links := map[string]string{}
	collectProfileURLs(user, links)
	if page, ok := user["subreddit"].(sourceObject); ok {
		collectProfileURLs(page, links)
	}
	// Resolution evidence is useful only for links actually present in this
	// profile, never for unrelated links found elsewhere on a fetched page.
	applyProfileResolutions(user, links)
	keys := make([]string, 0, len(links))
	for key := range links {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		ret.URLs = append(ret.URLs, links[key])
	}
	return ret, nil
}

func applyProfileResolutions(profile sourceObject, links map[string]string) {
	items, _ := profile["resolved_links"].([]interface{})
	for _, item := range items {
		entry, _ := item.(sourceObject)
		from, _ := entry["url"].(string)
		to, _ := entry["resolved_url"].(string)
		fromKey, toKey := ProfileURLKey(from), ProfileURLKey(to)
		if _, ok := links[fromKey]; ok && toKey != "" && fromKey != toKey {
			delete(links, fromKey)
			links[toKey] = to
		}
	}
}
