package scrape_test

import (
	"encoding/json"
	"testing"

	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

func lookupEvidence(t *testing.T, platform, candidate string, alter func(map[string]any)) map[string]any {
	t.Helper()
	evidence := map[string]any{"platform": platform, "account_key": nil, "candidate_url": candidate,
		"strict_filename_id": false, "paths": []string{"Unattributed/photo.jpg"},
		"titles": []string{}, "texts": []string{}, "dates": []string{}, "urls": []string{}}
	if alter != nil {
		alter(evidence)
	}
	body, err := json.Marshal(evidence)
	require.NoError(t, err)
	return map[string]any{"catalog_id": "c_11111111111111111111111111111111", "post_key": "legacy:post:original",
		"job_key": nil, "status": "lookup", "evidence_json": string(body)}
}

func TestDiscoveryLookupRequiresCorroborationAndTheExactSourcePost(t *testing.T) {
	for _, fixture := range []struct {
		name, platform, url, raw, basis string
		alter                           func(map[string]any)
	}{
		{"url alone", "reddit", "https://www.reddit.com/comments/abc123", `{"category":"reddit","id":"abc123"}`, "", nil},
		{"captured handle", "reddit", "https://www.reddit.com/comments/abc123", `{"category":"reddit","id":"abc123","author":"RIVER","author_fullname":"t2_one"}`, "captured-account-and-post-id",
			func(e map[string]any) { e["account_key"] = "reddit:handle:river" }},
		{"captured id", "reddit", "https://www.reddit.com/comments/abc123", `{"category":"reddit","id":"abc123","author":"new-name","author_fullname":"t2_one"}`, "captured-account-and-post-id",
			func(e map[string]any) { e["account_key"] = "reddit:id:t2_one" }},
		{"wrong id", "reddit", "https://www.reddit.com/comments/abc123", `{"category":"reddit","id":"abc123","author":"river","author_fullname":"t2_other"}`, "",
			func(e map[string]any) { e["account_key"] = "reddit:id:t2_one" }},
		{"feed profile is not publisher", "reddit", "https://www.reddit.com/comments/abc123", `{"category":"reddit","id":"abc123","user":{"name":"river","id":"one"}}`, "",
			func(e map[string]any) { e["account_key"] = "reddit:handle:river" }},
		{"another post", "reddit", "https://www.reddit.com/comments/abc123", `{"category":"reddit","id":"other","author":"river"}`, "",
			func(e map[string]any) { e["account_key"] = "reddit:handle:river" }},
		{"strict Twitter filename", "twitter", "https://x.com/i/web/status/123456789012345", `{"category":"twitter","tweet_id":"123456789012345","author":{"name":"aggregated-publisher","id":"2"}}`, "strict-filename-id",
			func(e map[string]any) {
				e["strict_filename_id"] = true
				e["paths"] = []string{"Feed/123456789012345_1.mp4", "Feed/123456789012345_2.jpg"}
			}},
		{"forged strict flag", "twitter", "https://x.com/i/web/status/123456789012345", `{"category":"twitter","tweet_id":"123456789012345"}`, "",
			func(e map[string]any) { e["strict_filename_id"] = true }},
		{"mixed file identities", "twitter", "https://x.com/i/web/status/123456789012345", `{"category":"twitter","tweet_id":"123456789012345"}`, "",
			func(e map[string]any) {
				e["strict_filename_id"] = true
				e["paths"] = []string{"123456789012345_1.mp4", "987654321098765_2.jpg"}
			}},
		{"matching original title and UTC date", "reddit", "https://www.reddit.com/comments/abc123", `{"category":"reddit","id":"abc123","title":"<b>Ａ distinctive</b> title &amp; enough words","date":"2025-01-01T23:30:00-02:00"}`, "title-date-and-post-id",
			func(e map[string]any) {
				e["dates"] = []string{"2025-01-02"}
				e["titles"] = []string{"a distinctive title enough words"}
			}},
		{"wrong UTC date", "reddit", "https://www.reddit.com/comments/abc123", `{"category":"reddit","id":"abc123","title":"A distinctive title with enough words","date":"2025-01-01T23:30:00-02:00"}`, "",
			func(e map[string]any) {
				e["dates"] = []string{"2025-01-01"}
				e["titles"] = []string{"a distinctive title with enough words"}
			}},
		{"Unicode length counts characters", "reddit", "https://www.reddit.com/comments/abc123", `{"category":"reddit","id":"abc123","title":"一二三四五六七八九十","date":"2025-01-02"}`, "",
			func(e map[string]any) {
				e["dates"] = []string{"2025-01-02"}
				e["titles"] = []string{"一二三四五六七八九十"}
			}},
		{"title without date", "reddit", "https://www.reddit.com/comments/abc123", `{"category":"reddit","id":"abc123","title":"A distinctive title with enough words"}`, "",
			func(e map[string]any) { e["titles"] = []string{"a distinctive title with enough words"} }},
		{"source parent owns child", "reddit", "https://www.reddit.com/comments/abc123", `{"category":"imgur","id":"child","_parent":{"category":"reddit","id":"abc123","author":"river","author_fullname":"t2_one"}}`, "captured-account-and-post-id",
			func(e map[string]any) { e["account_key"] = "reddit:handle:river" }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			values := lookupEvidence(t, fixture.platform, fixture.url, fixture.alter)
			post, basis, err := scrape.MatchDiscoveryLookup(values, json.RawMessage(fixture.raw))
			require.NoError(t, err)
			require.Equal(t, fixture.basis, basis)
			if basis == "" {
				require.Nil(t, post)
			} else {
				require.NotNil(t, post)
				require.Equal(t, "native:"+fixture.platform, post.Namespace)
			}
		})
	}
}
