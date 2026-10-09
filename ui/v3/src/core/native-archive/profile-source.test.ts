import { expect, it } from "vitest";
import { redditProfileURL } from "./profile-source";

it("uses the profile URL for its standard Reddit retrieval passes", () => {
  for (const target of [
    "https://www.reddit.com/user/Example/submitted/?sort=new",
    "https://www.reddit.com/user/example/submitted/?sort=top&t=year",
    "https://www.reddit.com/search?q=author%3AEXAMPLE+nsfw%3Ayes&include_over_18=on&sort=top&t=all",
    "https://reddit.com/u/example",
  ])
    expect(redditProfileURL(target)).toBe(
      "https://www.reddit.com/user/example/",
    );
});

it("preserves separate searches, saved feeds and subreddit sources", () => {
  for (const target of [
    "https://www.reddit.com/search?q=author%3Aexample+subreddit%3Atest",
    "https://www.reddit.com/user/me/saved/?sort=new",
    "https://www.reddit.com/r/example/",
    "https://www.reddit.com/user/example/?sort=new&sort=top",
    "https://www.reddit.com/user/example/?custom=1",
    "https://user:password@www.reddit.com/user/example/",
  ])
    expect(redditProfileURL(target)).toBeUndefined();
});
