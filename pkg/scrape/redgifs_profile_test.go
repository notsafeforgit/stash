package scrape_test

import (
	"testing"

	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

func TestRedgifsProfileURL(t *testing.T) {
	for _, value := range []string{"https://www.redgifs.com/users/Example", "http://redgifs.com/users/Example/"} {
		require.Equal(t, "https://www.redgifs.com/users/example", scrape.RedgifsProfileURL(value))
	}
	for _, value := range []string{"https://www.redgifs.com/watch/example", "https://api.redgifs.com/v2/users/example/search",
		"https://redgifs.com/users/example/collections", "https://redgifs.com/users/example?other=1", "https://redgifs.com/users/example#other",
		"https://user@redgifs.com/users/example", "https://redgifs.com:443/users/example", "https://redgifs.com/users/ex%61mple",
		"https://redgifs.com.evil.invalid/users/example", ""} {
		require.Empty(t, scrape.RedgifsProfileURL(value), value)
	}
}
