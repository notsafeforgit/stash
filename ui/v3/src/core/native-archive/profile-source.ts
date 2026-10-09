// The subscription identifies a profile; sorting/search variants are retrievals.
export function redditProfileURL(raw: string): string | undefined {
  try {
    const url = new URL(raw);
    if (
      url.protocol !== "https:" ||
      url.username ||
      url.password ||
      url.hash ||
      url.port ||
      !["reddit.com", "www.reddit.com", "old.reddit.com"].includes(url.hostname)
    )
      return;
    const query = url.searchParams;
    for (const key of query.keys()) if (query.getAll(key).length !== 1) return;
    const parts = url.pathname.replace(/^\/+|\/+$/g, "").split("/");
    let handle: string | undefined;
    if (
      (parts.length === 2 ||
        (parts.length === 3 && parts[2] === "submitted")) &&
      (parts[0] === "user" || parts[0] === "u")
    )
      handle = parts[1];
    else if (
      parts.length === 1 &&
      parts[0] === "search" &&
      query.get("include_over_18") === "on"
    ) {
      handle = /^author:([A-Za-z0-9_-]{1,32}) nsfw:yes$/.exec(
        query.get("q") ?? "",
      )?.[1];
      query.delete("q");
      query.delete("include_over_18");
    } else return;
    if (!handle || !/^[A-Za-z0-9_-]{1,32}$/.test(handle)) return;
    const sort = query.get("sort") ?? "",
      period = query.get("t") ?? "";
    query.delete("sort");
    query.delete("t");
    if (
      query.size ||
      !["", "new", "top"].includes(sort) ||
      (!["", "all"].includes(period) && !(sort === "top" && period === "year"))
    )
      return;
    return `https://www.reddit.com/user/${handle.toLowerCase()}/`;
  } catch {
    return;
  }
}
