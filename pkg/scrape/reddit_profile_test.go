package scrape

import "testing"

func TestRedditProfileURL(t *testing.T) {
	for _, raw := range []string{
		"https://www.reddit.com/user/Example/", "https://reddit.com/u/EXAMPLE",
		"https://www.reddit.com/user/Example/submitted/?sort=new",
		"https://www.reddit.com/user/example/submitted/?sort=top&t=year",
		"https://www.reddit.com/search?q=author%3AExample+nsfw%3Ayes&include_over_18=on&sort=new&t=all",
	} {
		if got := RedditProfileURL(raw); got != "https://www.reddit.com/user/example/" {
			t.Errorf("%s: %s", raw, got)
		}
	}
	for _, raw := range []string{
		"https://www.reddit.com/user/me/saved/?sort=new", "https://www.reddit.com/user/example/comments/",
		"https://www.reddit.com/search?q=author%3Aexample+subreddit%3Atest&sort=new",
		"https://www.reddit.com/r/example/?sort=new", "https://www.reddit.com/user/example/?sort=hot",
		"https://www.reddit.com/user/example/?sort=new&sort=top", "https://www.reddit.com/user/example/?unknown=1",
		"https://evil.invalid/user/example/", "https://user:pass@www.reddit.com/user/example/",
	} {
		if got := RedditProfileURL(raw); got != "" {
			t.Errorf("must not combine %s: %s", raw, got)
		}
	}
}
