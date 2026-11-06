package sourceadapter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCapturedRedgifsGalleryMembershipRequiresLinkedSeed(t *testing.T) {
	member := []byte(`{"category":"redgifs","id":"extra","gallery":"gallery-id","source_extractor_url":"https://i.redgifs.com/i/seed.jpg"}`)
	for _, tc := range []struct {
		name string
		edit func(sourceObject)
		want bool
	}{
		{"linked seed", func(sourceObject) {}, true},
		{"another member", func(p sourceObject) { p["id"] = "other" }, false},
		{"another gallery", func(p sourceObject) { p["gallery"] = "other" }, false},
		{"unrelated Reddit link", func(p sourceObject) { p["_reddit"].(sourceObject)["url"] = "https://i.redgifs.com/i/other.jpg" }, false},
		{"missing Reddit context", func(p sourceObject) { delete(p, "_reddit") }, false},
		{"crosspost", func(p sourceObject) {
			original := p["_reddit"]
			p["_reddit"] = sourceObject{"id": "crosspost", "url": "https://www.reddit.com/comments/original/",
				"crosspost_parent": "t3_original", "crosspost_parent_list": []any{original}}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := sourceObject{"category": "redgifs", "id": "seed", "gallery": "gallery-id",
				"_reddit": sourceObject{"id": "original", "url": "https://i.redgifs.com/i/seed.jpg"}}
			tc.edit(parent)
			raw, err := json.Marshal(parent)
			require.NoError(t, err)
			allowed, err := CapturedRedgifsGalleryMember(raw, member)
			require.NoError(t, err)
			require.Equal(t, tc.want, allowed)
		})
	}
}
