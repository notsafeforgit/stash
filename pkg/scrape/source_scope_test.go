package scrape_test

import (
	"testing"

	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

func TestSourceScopeUsesContactedServiceInsteadOfCreatorOrMirrorAccount(t *testing.T) {
	for _, item := range []struct{ url, scope string }{
		{"https://www.reddit.com/user/one", "service:reddit"},
		{"http://old.reddit.com/r/two", "service:reddit"},
		{"https://X.com/one/status/123", "service:twitter"},
		{"https://twitter.com/two", "service:twitter"},
		{"https://coomer.st/onlyfans/user/one", "mirror:coomer"},
		{"https://coomer.su/fansly/user/two", "mirror:coomer"},
		{"https://kemono.cr/patreon/user/123", "mirror:kemono"},
		{"https://kemono.party/fanbox/user/123", "mirror:kemono"},
		{"https://onlyfans.com/one", "service:onlyfans"},
		{"https://bsky.app/profile/one", "service:bluesky"},
		{"https://www.tiktok.com/@one", "service:tiktok"},
		{"https://www.instagram.com/one", "service:instagram"},
		{"https://www.redgifs.com/ifr/one", "service:redgifs"},
		{"https://i.imgur.com/one", "service:imgur"},
		{"https://reddit.com.example.org/one", "host:reddit.com.example.org"},
		{"https://one.example.org/one", "host:one.example.org"},
		{"https://two.example.org/two", "host:two.example.org"},
		{"https://WWW.Example.org.:8443/one", "host:example.org"},
		{"https://bücher.example/one", "host:xn--bcher-kva.example"},
	} {
		scope, err := scrape.SourceScopeV1(item.url)
		require.NoError(t, err, item.url)
		require.Equal(t, item.scope, scope, item.url)
	}
	for _, raw := range []string{"", "relative/path", "file:///tmp/one", "https://private:secret@example.org/", "https://"} {
		_, err := scrape.SourceScopeV1(raw)
		require.Error(t, err, raw)
	}
}
