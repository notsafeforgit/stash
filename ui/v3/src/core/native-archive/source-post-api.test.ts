import { expect, it, vi } from "vitest";
import { createSourcePostAPI, postFilterSchema } from "./source-post-api";
import {
  postSummary,
  postMedia,
  postAlbum,
  postIds,
  postIdentity,
  postAlbumContext,
} from "../../../tests/fixtures/source-posts";
import { account } from "../../../tests/fixtures/account-review";

const endpoint = "https://example.test/stash/api/v3/archive/";
const browse = postFilterSchema.parse({});
function client(value: unknown) {
  const transport = vi.fn<typeof fetch>(async () => Response.json(value));
  return { api: createSourcePostAPI(endpoint, transport), transport };
}

it("reads exact thread IDs without rounding and rejects unsafe links and stale cursors", async () => {
  const id = "1900000000000000003";
  const thread = {
    post_uuid: postIds.post,
    conflict: false,
    facts: {
      namespace: "native:twitter",
      post_id: id,
      conversation_id: "1900000000000000001",
      reply_id: "1900000000000000002",
      author_id: "99",
      reply_author_id: "99",
    },
    root: {
      source_id: "1900000000000000001",
      url: "https://x.com/i/status/1900000000000000001",
      post: null,
    },
    parent: null,
    posts: [
      {
        source_id: id,
        url: `https://x.com/i/status/${id}`,
        post: postSummary(),
      },
    ],
    next: id,
  };
  const response = { requested_uuid: postIds.post, thread };
  const { api, transport } = client(response);
  expect((await api.thread(postIds.post)).next).toBe(id);
  expect(transport.mock.calls[0]?.[1]?.method).toBe("GET");
  await expect(api.thread(postIds.post, id)).rejects.toThrow();
  await expect(api.thread(postIds.otherPost)).rejects.toThrow();
  await expect(
    client({
      ...response,
      thread: { ...thread, root: { ...thread.root, url: "javascript:bad" } },
    }).api.thread(postIds.post),
  ).rejects.toThrow();
  await expect(
    client({
      ...response,
      thread: { ...thread, next: "1900000000000000004" },
    }).api.thread(postIds.post),
  ).rejects.toThrow();
});

it("checks both the requested post identity and the current canonical identity", async () => {
  const value = postIdentity(postIds.post, postIds.otherPost, 7);
  const { api, transport } = client(value);
  expect(await api.identity(postIds.post)).toEqual(value);
  expect(String(transport.mock.calls[0]?.[0])).toBe(
    `${endpoint}posts/${postIds.post}/identity`,
  );
  await expect(api.identity(postIds.otherPost)).rejects.toMatchObject({
    code: "invalid_response",
  });
  for (const invalid of [
    { ...value, canonical: { ...value.canonical, uuid: postIds.post } },
    { ...value, canonical: { ...value.canonical, redirect_to: postIds.post } },
    { ...value, requested: { ...value.requested, redirect_to: null } },
  ])
    await expect(client(invalid).api.identity(postIds.post)).rejects.toThrow();
});

it("keeps deployment prefixes, exact URL query contents and session-only read requests", async () => {
  const { api, transport } = client([postSummary()]);
  const exact = "https://example.test/post?a=one&b=two#part";
  await api.posts({ ...browse, mode: "url", value: exact });
  const [target, options] = transport.mock.calls[0] ?? [];
  const url = new URL(String(target));
  expect(url.pathname).toBe("/stash/api/v3/archive/posts");
  expect(url.searchParams.get("url")).toBe(exact);
  expect(url.searchParams.get("limit")).toBe("25");
  expect(options).toMatchObject({
    method: "GET",
    cache: "no-store",
    credentials: "same-origin",
    redirect: "error",
  });
  expect(options?.body).toBeUndefined();
});

it("qualifies a source ID and carries UUID cursors without changing selector semantics", async () => {
  const { api, transport } = client([]);
  await api.posts(
    { mode: "source_id", value: "123", namespace: "mirror:coomer:onlyfans" },
    postIds.post,
  );
  const url = new URL(String(transport.mock.calls[0]?.[0]));
  expect(Object.fromEntries(url.searchParams)).toEqual({
    limit: "25",
    after: postIds.post,
    namespace: "mirror:coomer:onlyfans",
    value: "123",
  });
});

it.each([
  { mode: "url", value: "javascript:alert(1)" },
  { mode: "url", value: "https://user:password@example.test/post" },
  { mode: "url", value: " https://example.test/post" },
  { mode: "uuid", value: "1" },
  { mode: "source_id", value: "123", namespace: "twitter" },
  { mode: "source_id", value: "", namespace: "native:twitter" },
])("rejects invalid lookups before sending requests: %j", async (filter) => {
  const { api, transport } = client([]);
  await expect(
    api.posts({ ...browse, ...filter } as typeof browse),
  ).rejects.toThrow();
  expect(transport).not.toHaveBeenCalled();
});

it("preserves multiple posts sharing one source URL and rejects duplicate/reversed pages", async () => {
  const first = postSummary();
  const second = {
    ...first,
    requested_uuid: postIds.otherPost,
    uuid: postIds.otherPost,
    urls: first.urls.map((u) => ({ ...u, post_uuid: postIds.otherPost })),
  };
  expect(
    await client([first, second]).api.posts({
      ...browse,
      mode: "url",
      value: first.urls[0]?.url ?? "",
    }),
  ).toHaveLength(2);
  for (const rows of [
    [first, first],
    [second, first],
    Array.from({ length: 26 }, () => first),
  ])
    await expect(client(rows).api.posts(browse)).rejects.toThrow();
  await expect(client([first]).api.posts(browse, first.uuid)).rejects.toThrow();
  await expect(
    client([second]).api.posts({ ...browse, mode: "uuid", value: first.uuid }),
  ).rejects.toThrow();
});

it("requires the requested scope while retaining original URL and capture owners after consolidation", async () => {
  const row = postSummary();
  expect(await client(row).api.post(row.uuid)).toEqual(row);
  await expect(client(row).api.post(postIds.otherPost)).rejects.toThrow();
  const merged = { ...row, uuid: postIds.otherPost };
  expect(await client(merged).api.post(row.uuid)).toEqual(merged);
  expect(
    await client([merged]).api.posts({
      ...browse,
      mode: "uuid",
      value: row.uuid,
    }),
  ).toEqual([merged]);
  await expect(client([merged]).api.posts(browse)).rejects.toThrow();
  await expect(
    client({ ...row, urls: [row.urls[0], row.urls[0]] }).api.post(row.uuid),
  ).rejects.toThrow();
});

it("reads canonical media choices through an original post link and checks the requested scope", async () => {
  const row = postMedia();
  row.association.post_uuid = postIds.otherPost;
  expect(await client([row]).api.media(postIds.post)).toEqual([row]);
  await expect(client([row]).api.media(postIds.otherPost)).rejects.toThrow();
});

it("keeps unknown observation time separate from archive recording time", async () => {
  const row = postSummary();
  row.latest_capture = {
    ...row.latest_capture!,
    captured_at: null,
    recorded_at: "2026-10-06T01:00:00Z",
  };
  expect(
    (await client(row).api.post(row.uuid)).latest_capture?.captured_at,
  ).toBeNull();
  row.latest_capture.recorded_at = null;
  await expect(client(row).api.post(row.uuid)).rejects.toThrow();
});

it("checks selected canonical publisher pages and retains ownership choices", async () => {
  const selected = account();
  expect(await client([selected]).api.publishers(postIds.post)).toEqual([
    selected,
  ]);
  await expect(
    client([{ ...selected, canonical_uuid: postIds.otherPost }]).api.publishers(
      postIds.post,
    ),
  ).rejects.toThrow();
  await expect(
    client([selected, selected]).api.publishers(postIds.post),
  ).rejects.toThrow();
  await expect(
    client([selected]).api.publishers(postIds.post, selected.uuid),
  ).rejects.toThrow();
});

it("preserves suppressed media links and refuses cross-post or mismatched identities", async () => {
  const media = postMedia();
  media.association.state = "unlinked";
  media.linked_attachments = 3;
  expect(await client([media]).api.media(postIds.post)).toEqual([media]);
  await expect(client([media]).api.media(postIds.otherPost)).rejects.toThrow();
  await expect(
    client([
      { ...media, media: { ...media.media, uuid: postIds.otherPost } },
    ]).api.media(postIds.post),
  ).rejects.toThrow();
  await expect(
    client([media, media]).api.media(postIds.post),
  ).rejects.toThrow();
  media.media.state = media.association.media_state = "deleted";
  await expect(client([media]).api.media(postIds.post)).rejects.toThrow();
  media.media.local_id = null;
  expect(
    (await client([media]).api.media(postIds.post))[0]?.media.local_id,
  ).toBeNull();
});

it("preserves disabled/deleted albums and resolves gallery aliases without inventing an active local ID", async () => {
  const album = postAlbum();
  expect(
    await client(postAlbumContext(null)).api.album(postIds.post),
  ).toBeNull();
  expect(
    await client(
      postAlbumContext({
        ...album,
        state: "disabled",
        gallery: null,
        gallery_uuid: null,
      }),
    ).api.album(postIds.post),
  ).toMatchObject({ state: "disabled" });
  await expect(
    client(postAlbumContext({ ...album, state: "disabled" })).api.album(
      postIds.post,
    ),
  ).rejects.toThrow();
  album.gallery_uuid = postIds.otherPost;
  expect(await client(postAlbumContext(album)).api.album(postIds.post)).toEqual(
    album,
  );
  await expect(
    client(postAlbumContext(album)).api.album(postIds.otherPost),
  ).rejects.toThrow();
  album.gallery!.state = "deleted";
  album.gallery!.local_id = null;
  expect(
    (await client(postAlbumContext(album)).api.album(postIds.post))?.gallery
      ?.state,
  ).toBe("deleted");
});

it("retains the requested scope while returning the merged post's gallery choice", async () => {
  const album = { ...postAlbum(), post_uuid: postIds.otherPost };
  expect(await client(postAlbumContext(album)).api.album(postIds.post)).toEqual(
    album,
  );
});

it("validates identifier pages using SQLite Unicode order and qualified cursors", async () => {
  const prior = { namespace: "native:reddit", value: "\ue000" };
  const next = { namespace: "native:reddit", value: "😀" };
  const { api, transport } = client([next]);
  expect(await api.identifiers(postIds.post, prior)).toEqual([next]);
  const query = new URL(String(transport.mock.calls[0]?.[0])).searchParams;
  expect(query.get("after_namespace")).toBe(prior.namespace);
  expect(query.get("after_value")).toBe(prior.value);
  await expect(
    client([prior]).api.identifiers(postIds.post, next),
  ).rejects.toThrow();
  await expect(
    client([next, next]).api.identifiers(postIds.post),
  ).rejects.toThrow();
});
