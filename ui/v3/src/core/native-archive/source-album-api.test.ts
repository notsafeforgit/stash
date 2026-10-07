import { expect, it, vi } from "vitest";
import { createSourceAlbumAPI } from "./source-album-api";
import { albumPage, albumSlot } from "../../../tests/fixtures/source-albums";
import {
  postIds,
  postSummary,
  postAlbum,
} from "../../../tests/fixtures/source-posts";

const endpoint = "https://example.test/stash/api/v3/archive/";
function client(value: unknown) {
  const transport = vi.fn<typeof fetch>(async () => Response.json(value));
  return { api: createSourceAlbumAPI(endpoint, transport), transport };
}

it("uses session reads and preserves mixed source order, repetitions and gaps", async () => {
  const expected = albumPage();
  const { api, transport } = client(expected);
  expect(await api.album(postIds.post)).toEqual(expected);
  const [url, options] = transport.mock.calls[0]!;
  expect(String(url)).toBe(
    `${endpoint}posts/${postIds.post}/album-media?limit=25`,
  );
  expect(options).toMatchObject({
    method: "GET",
    credentials: "same-origin",
    redirect: "error",
    cache: "no-store",
  });
  expect(options?.body).toBeUndefined();
});

it("accepts positional continuation and rejects invalid bounds before HTTP", async () => {
  const page = { ...albumPage(), slots: [albumSlot(6)] };
  const { api, transport } = client(page);
  expect((await api.album(postIds.post, 5)).slots[0]?.position).toBe(6);
  expect(String(transport.mock.calls[0]?.[0])).toContain("after=5");
  for (const after of [-1, 1.5, 1000000]) {
    const invalid = client(page);
    await expect(invalid.api.album(postIds.post, after)).rejects.toThrow();
    expect(invalid.transport).not.toHaveBeenCalled();
  }
});

it("rejects wrong post scopes, unexplained position gaps and duplicate positions", async () => {
  const original = albumPage();
  for (const value of [
    { ...original, requested_uuid: postIds.otherPost },
    { ...original, post_uuid: postIds.otherPost },
    { ...original, slots: [albumSlot(1)] },
    { ...original, slots: [albumSlot(0), albumSlot(0)] },
    { ...original, slots: [albumSlot(0), albumSlot(2)] },
    { ...original, next_after: 6 },
    { ...original, slots: [albumSlot(0)], selection: null },
    { ...original, album: { ...postAlbum(), post_uuid: postIds.otherPost } },
  ])
    await expect(client(value).api.album(postIds.post)).rejects.toThrow();
});

it("reads a merged album through its original post while checking the requested scope", async () => {
  const original = albumPage();
  const merged = {
    ...original,
    post_uuid: postIds.otherPost,
    album: { ...postAlbum(), post_uuid: postIds.otherPost },
  };
  expect(await client(merged).api.album(postIds.post)).toEqual(merged);
  await expect(
    client(merged).api.album(postIds.otherPost),
  ).rejects.toMatchObject({
    code: "invalid_response",
  });
});

it("does not turn rejected selections or deleted media into active file links", async () => {
  const original = albumPage();
  const slot = albumSlot(0);
  for (const broken of [
    { ...slot, selection_state: "unlinked" },
    { ...slot, attachment: null },
    { ...slot, through: 1 },
    { ...slot, post_link_state: "" },
    { ...slot, decision_uuid: null },
    { ...slot, media: { ...slot.media!, state: "deleted", local_id: null } },
    { ...slot, media: { ...slot.media!, kind: "gallery" } },
  ])
    await expect(
      client({ ...original, slots: [broken] }).api.album(postIds.post),
    ).rejects.toThrow();
  const deleted = {
    ...slot,
    media: { ...slot.media!, state: "deleted", local_id: null },
    registered_files: 0,
  };
  expect(
    (await client({ ...original, slots: [deleted] }).api.album(postIds.post))
      .slots[0]?.media?.local_id,
  ).toBeNull();
});

it("does not conflate a complete source list with locally registered files", async () => {
  const page = albumPage();
  page.selection = {
    ...page.selection!,
    complete: true,
    expected_count: 1,
    entry_count: 1,
  };
  page.slots = [
    {
      ...albumSlot(0),
      selection_state: "unselected",
      decision_uuid: null,
      media: null,
      post_link_state: "",
      gallery_membership: "absent",
      registered_files: 0,
    },
  ];
  expect((await client(page).api.album(postIds.post)).selection?.complete).toBe(
    true,
  );
  await expect(
    client({
      ...page,
      selection: { ...page.selection, expected_count: 2 },
    }).api.album(postIds.post),
  ).rejects.toThrow();
});

it("follows gallery redirects but validates requested identity and post cursor scope", async () => {
  const page = {
    requested_uuid: postIds.gallery,
    gallery: postAlbum().gallery,
    posts: [postSummary()],
  };
  expect((await client(page).api.posts(postIds.gallery)).posts).toHaveLength(1);
  await expect(
    client(page).api.posts(postIds.gallery, postIds.post),
  ).rejects.toThrow();
  await expect(
    client({ ...page, requested_uuid: postIds.post }).api.posts(
      postIds.gallery,
    ),
  ).rejects.toThrow();
  await expect(
    client({ ...page, posts: [postSummary(), postSummary()] }).api.posts(
      postIds.gallery,
    ),
  ).rejects.toThrow();
  const { api } = client({
    uuid: postIds.gallery,
    revision: 1,
    kind: "gallery",
    local_id: 12,
  });
  expect((await api.identity("12")).uuid).toBe(postIds.gallery);
  await expect(api.identity("13")).rejects.toThrow();
});
