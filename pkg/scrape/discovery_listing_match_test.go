package scrape_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

func listingEvidence(t *testing.T, platform string, alter func(map[string]any)) map[string]any {
	t.Helper()
	value := lookupEvidence(t, platform, "", func(e map[string]any) {
		e["candidate_url"], e["account_key"] = nil, platform+":id:123"
		if platform == "reddit" {
			e["account_key"] = "reddit:id:t2_abc"
		}
		if alter != nil {
			alter(e)
		}
	})
	value["job_key"], value["status"] = strings.Repeat("a", 64), "pending"
	return value
}

func TestDiscoveryListingRequiresOriginalCorroboration(t *testing.T) {
	const title = "a distinctive original album title"
	const text = "this is the retained original description with enough separate words to corroborate the title"
	for _, test := range []struct {
		name, platform, raw, basis string
		weak                       bool
		alter                      func(map[string]any)
	}{
		{name: "same account alone", raw: `{"category":"reddit","id":"abc123","author":"juniper","author_fullname":"t2_abc"}`},
		{name: "date alone", raw: `{"category":"reddit","id":"abc123","date":"2025-01-02"}`,
			alter: func(e map[string]any) { e["dates"] = []string{"2025-01-02"} }},
		{name: "strict filename is not a listing shortcut", platform: "twitter", raw: `{"category":"twitter","tweet_id":"123456789012345","author":{"id":"123","name":"juniper"}}`,
			alter: func(e map[string]any) { e["strict_filename_id"], e["paths"] = true, []string{"123456789012345_1.jpg"} }},
		{name: "title and UTC source day", raw: `{"category":"reddit","id":"abc123","title":"<b>Ａ distinctive</b> original album title!","date":"2025-01-01T23:30:00-02:00"}`, basis: "exact-title-and-date",
			alter: func(e map[string]any) { e["titles"], e["dates"] = []string{title}, []string{"2025-01-02"} }},
		{name: "text and day", raw: `{"category":"reddit","id":"abc123","selftext":"` + text + `","date":"2025-01-02"}`, basis: "exact-original-text-and-date",
			alter: func(e map[string]any) { e["texts"], e["dates"] = []string{text}, []string{"2025-01-02"} }},
		{name: "distinct title and original text", raw: `{"category":"reddit","id":"abc123","title":"` + title + `","selftext":"` + text + `"}`, basis: "exact-title-and-original-text",
			alter: func(e map[string]any) { e["titles"], e["texts"] = []string{title}, []string{text} }},
		{name: "copied title is one piece of evidence", raw: `{"category":"reddit","id":"abc123","title":"` + text + `","selftext":"` + text + `"}`, basis: "title-needs-verification", weak: true,
			alter: func(e map[string]any) { e["titles"], e["texts"] = []string{text}, []string{text} }},
		{name: "title only stays weak", raw: `{"category":"reddit","id":"abc123","title":"` + title + `"}`, basis: "title-needs-verification", weak: true,
			alter: func(e map[string]any) { e["titles"] = []string{title} }},
		{name: "wrong day does not corroborate title", raw: `{"category":"reddit","id":"abc123","title":"` + title + `","date":"2025-01-03"}`, basis: "title-needs-verification", weak: true,
			alter: func(e map[string]any) { e["titles"], e["dates"] = []string{title}, []string{"2025-01-02"} }},
		{name: "observation time is not post date", raw: `{"category":"reddit","id":"abc123","title":"` + title + `","captured_at":"2025-01-02"}`, basis: "title-needs-verification", weak: true,
			alter: func(e map[string]any) { e["titles"], e["dates"] = []string{title}, []string{"2025-01-02"} }},
		{name: "original post URL and day", raw: `{"category":"reddit","id":"abc123","date":"2025-01-02"}`, basis: "exact-source-url-and-date",
			alter: func(e map[string]any) {
				e["urls"], e["dates"] = []string{"https://www.reddit.com/r/example/comments/abc123/original/"}, []string{"2025-01-02"}
			}},
		{name: "asset URL is not source post identity", raw: `{"category":"reddit","id":"abc123","date":"2025-01-02","url":"https://i.redd.it/one.jpg"}`,
			alter: func(e map[string]any) {
				e["urls"], e["dates"] = []string{"https://i.redd.it/one.jpg"}, []string{"2025-01-02"}
			}},
		{name: "another post URL", raw: `{"category":"reddit","id":"abc123","date":"2025-01-02"}`,
			alter: func(e map[string]any) {
				e["urls"], e["dates"] = []string{"https://www.reddit.com/comments/other"}, []string{"2025-01-02"}
			}},
		{name: "contradictory publisher ID", raw: `{"category":"reddit","id":"abc123","author_fullname":"t2_other","author":"juniper","title":"` + title + `","date":"2025-01-02"}`,
			alter: func(e map[string]any) { e["titles"], e["dates"] = []string{title}, []string{"2025-01-02"} }},
		{name: "renamed handle preserves known ID", platform: "twitter", raw: `{"category":"twitter","tweet_id":"123456789012345","author":{"id":"123","name":"new_name"},"title":"` + title + `","date":"2025-01-02"}`, basis: "exact-title-and-date",
			alter: func(e map[string]any) { e["titles"], e["dates"] = []string{title}, []string{"2025-01-02"} }},
		{name: "feed owner alone is not evidence", raw: `{"category":"reddit","id":"abc123","user":{"name":"juniper","id":"abc"}}`},
		{name: "child uses source caption", raw: `{"category":"redgifs","id":"child","title":"other title","_parent":{"category":"reddit","id":"abc123","title":"` + title + `","date":"2025-01-02"}}`, basis: "exact-title-and-date",
			alter: func(e map[string]any) { e["titles"], e["dates"] = []string{title}, []string{"2025-01-02"} }},
		{name: "cross service is not interchangeable", raw: `{"category":"twitter","tweet_id":"123456789012345","title":"` + title + `","date":"2025-01-02"}`,
			alter: func(e map[string]any) { e["titles"], e["dates"] = []string{title}, []string{"2025-01-02"} }},
		{name: "Unicode threshold counts characters", raw: `{"category":"reddit","id":"abc123","title":"一二三四五六七八九十","date":"2025-01-02"}`,
			alter: func(e map[string]any) {
				e["titles"], e["dates"] = []string{"一二三四五六七八九十"}, []string{"2025-01-02"}
			}},
		{name: "translation is not original evidence", raw: `{"category":"reddit","id":"abc123","title":"different original","translated_text":"` + text + `","date":"2025-01-02"}`,
			alter: func(e map[string]any) { e["texts"], e["dates"] = []string{text}, []string{"2025-01-02"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			platform := test.platform
			if platform == "" {
				platform = "reddit"
			}
			values := listingEvidence(t, platform, test.alter)
			before, err := json.Marshal(values)
			require.NoError(t, err)
			candidate, err := scrape.MatchDiscoveryListing(values, json.RawMessage(test.raw))
			require.NoError(t, err)
			if test.basis == "" {
				require.Nil(t, candidate)
			} else {
				require.NotNil(t, candidate)
				require.Equal(t, test.basis, candidate.Basis)
				require.Equal(t, test.weak, candidate.NeedsDetail)
				require.Equal(t, scrape.DiscoveryListingMatchPolicy, candidate.Policy)
				require.Equal(t, "native:"+platform, candidate.Post.Namespace)
			}
			after, err := json.Marshal(values)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestDiscoveryWeakDetailCannotBorrowLookupShortcuts(t *testing.T) {
	values := listingEvidence(t, "twitter", func(e map[string]any) {
		e["titles"] = []string{"a distinctive original album title"}
		e["dates"] = []string{"2025-01-02"}
		e["strict_filename_id"], e["paths"] = true, []string{"123456789012345_1.jpg"}
	})
	weak := json.RawMessage(`{"category":"twitter","tweet_id":"123456789012345","author":{"id":"123","name":"juniper"},"title":"A distinctive original album title"}`)
	candidate, err := scrape.MatchDiscoveryListing(values, weak)
	require.NoError(t, err)
	require.True(t, candidate.NeedsDetail)
	for range 2 {
		detail, err := scrape.MatchDiscoveryListing(values, weak)
		require.NoError(t, err)
		require.Equal(t, candidate, detail, "confirming the inferred URL and account cannot manufacture corroboration")
	}
	detail, err := scrape.MatchDiscoveryListing(values, json.RawMessage(`{"category":"twitter","tweet_id":"123456789012345","author":{"id":"123","name":"juniper"},"title":"A distinctive original album title","date":"2025-01-02"}`))
	require.NoError(t, err)
	require.False(t, detail.NeedsDetail, "the detail must corroborate original evidence")
	require.Equal(t, "exact-title-and-date", detail.Basis)
	lookup := lookupEvidence(t, "twitter", candidate.URL, func(e map[string]any) {
		e["strict_filename_id"], e["paths"] = true, []string{"123456789012345_1.jpg"}
	})
	post, basis, err := scrape.MatchDiscoveryLookup(lookup, weak)
	require.NoError(t, err)
	require.NotNil(t, post)
	require.Equal(t, "strict-filename-id", basis)
	_, err = scrape.MatchDiscoveryListing(lookup, weak)
	require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
}

func listingPages(t *testing.T) []json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("../archive/testdata/discovery-pages-v1.json")
	require.NoError(t, err)
	var corpus struct {
		Pages []struct {
			Page json.RawMessage `json:"page"`
		} `json:"pages"`
	}
	require.NoError(t, json.Unmarshal(raw, &corpus))
	ret := []json.RawMessage{}
	for _, page := range corpus.Pages {
		ret = append(ret, page.Page)
	}
	return ret
}

func TestDiscoveryPageSharesAlbumCandidatesAndRetainsCompetingPosts(t *testing.T) {
	pages := listingPages(t)
	twitter := listingEvidence(t, "twitter", func(e map[string]any) {
		e["titles"], e["dates"] = []string{"shared original caption 合集"}, []string{"2026-10-03"}
	})
	candidates, err := scrape.MatchDiscoveryPage(twitter, pages[0])
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "9223372036854775815", candidates[0].Post.Value)
	require.Equal(t, []int{0, 1, 2, 3}, candidates[0].RecordOrdinals)
	require.False(t, candidates[0].NeedsDetail)

	reddit := listingEvidence(t, "reddit", func(e map[string]any) {
		e["titles"], e["dates"] = []string{"album with source positions"}, []string{"2026-10-03"}
	})
	for _, raw := range pages[1:3] {
		candidates, err = scrape.MatchDiscoveryPage(reddit, raw)
		require.NoError(t, err)
		require.Len(t, candidates, 1)
		require.True(t, candidates[0].NeedsDetail)
		require.Equal(t, []int{0, 1, 2}, candidates[0].RecordOrdinals)
	}
	page, err := archive.DecodeJSONObject(pages[1], archive.MaxDiscoveryPageBytes)
	require.NoError(t, err)
	records := page["records"].([]any)
	records[1].(map[string]any)["patch"].(map[string]any)["date"] = "2026-10-03"
	page["records"] = append(records, map[string]any{"kind": "post", "base": json.Number("0"), "parent": nil,
		"observed_at": "2026-10-04T01:02:03.000000004Z", "patch": map[string]any{"id": "def456", "date": "2026-10-03"}, "removed": []any{}})
	encoded, err := archive.EncodeSourceJSON(page)
	require.NoError(t, err)
	candidates, err = scrape.MatchDiscoveryPage(reddit, encoded)
	require.NoError(t, err)
	require.Len(t, candidates, 2, "identical captions on different posts retain competing identities")
	require.Equal(t, []int{0, 1, 2}, candidates[0].RecordOrdinals)
	require.False(t, candidates[0].NeedsDetail, "a source-date-bearing record corroborates the same post's weak title")
	require.Equal(t, []int{3}, candidates[1].RecordOrdinals)
	require.Equal(t, "def456", candidates[1].Post.Value)
	for _, raw := range pages[3:] {
		values := twitter
		if strings.Contains(string(raw), "reddit.com") {
			values = reddit
		}
		candidates, err = scrape.MatchDiscoveryPage(values, raw)
		require.NoError(t, err)
		require.Empty(t, candidates)
	}
	_, err = scrape.MatchDiscoveryPage(reddit, pages[0])
	require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
}

func TestDiscoveryListingRejectsMalformedAndForeignTargetEvidence(t *testing.T) {
	for name, alter := range map[string]func(map[string]any){
		"inferred candidate URL": func(e map[string]any) { e["candidate_url"] = "https://www.reddit.com/comments/abc123" },
		"unqualified account":    func(e map[string]any) { e["account_key"] = "juniper" },
		"wrong namespace":        func(e map[string]any) { e["account_key"] = "twitter:id:123" },
		"unknown reference kind": func(e map[string]any) { e["account_key"] = "reddit:guess:juniper" },
		"missing original paths": func(e map[string]any) { e["paths"] = []string{} },
	} {
		t.Run(name, func(t *testing.T) {
			_, err := scrape.MatchDiscoveryListing(listingEvidence(t, "reddit", alter), json.RawMessage(`{"category":"reddit","id":"abc123"}`))
			require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
		})
	}
	_, err := scrape.MatchDiscoveryListing(listingEvidence(t, "reddit", nil), json.RawMessage(`{"category":"reddit","id":"abc123","id":"def456"}`))
	require.Error(t, err)
	_, err = scrape.MatchDiscoveryPage(listingEvidence(t, "reddit", nil), json.RawMessage(`{"complete":true}`))
	require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
}
