import { expect, it, vi } from "vitest";
import {
  createAttachmentSelectionAPI,
  selectionListSchema,
} from "./attachment-selection-api";
import {
  ids,
  preview,
  receipt,
} from "../../../tests/fixtures/attachment-selection";

const endpoint = "https://example.test/library/api/v3/archive/";
function input() {
  return {
    ...preview().input,
    digest: preview().digest,
    request_uuid: ids.request,
  };
}

it("previews scoped source order using the deployment base and session transport", async () => {
  const transport = vi.fn<typeof fetch>(async () => Response.json(preview()));
  const api = createAttachmentSelectionAPI(endpoint, transport);
  expect(await api.preview(preview().input)).toEqual(preview());
  expect(String(transport.mock.calls[0]?.[0])).toBe(
    `${endpoint}attachment-selection/preview`,
  );
  expect(transport.mock.calls[0]?.[1]).toMatchObject({
    method: "POST",
    credentials: "same-origin",
    body: JSON.stringify(preview().input),
  });
});

it("preserves repeated media at distinct positions and validates list boundaries", () => {
  expect(selectionListSchema.parse(preview().proposed).entries).toHaveLength(2);
  const source = preview().proposed;
  expect(() =>
    selectionListSchema.parse({
      ...source,
      entries: [source.entries[0], source.entries[0]],
    }),
  ).toThrow();
  expect(() =>
    selectionListSchema.parse({ ...source, expected_count: 5 }),
  ).toThrow();
  expect(() =>
    selectionListSchema.parse({ ...source, complete: true, expected_count: 2 }),
  ).toThrow();
  expect(() =>
    selectionListSchema.parse({
      ...source,
      manifest_uuids: [ids.manifest, ids.manifest],
    }),
  ).toThrow();
  expect(selectionListSchema.parse(preview(true).proposed)).toMatchObject({
    mode: "disabled",
    entries: [],
  });
});

it("rejects previews for another post or a different proposed capture", async () => {
  const wrongPost = {
    ...preview(),
    input: { ...preview().input, post_uuid: ids.otherPost },
  };
  await expect(
    createAttachmentSelectionAPI(endpoint, async () =>
      Response.json(wrongPost),
    ).preview(preview().input),
  ).rejects.toMatchObject({ code: "preview_mismatch" });
  const wrongChoice = {
    ...preview(),
    proposed: { ...preview().proposed, capture_uuid: ids.otherPost },
  };
  await expect(
    createAttachmentSelectionAPI(endpoint, async () =>
      Response.json(wrongChoice),
    ).preview(preview().input),
  ).rejects.toThrow();
});

it("keeps the original serialized body and checks exact receipt identity", async () => {
  const body = JSON.stringify(input(), null, 2);
  const transport = vi.fn<typeof fetch>(async () =>
    Response.json({ review: receipt(input()), replayed: false }),
  );
  expect(
    (await createAttachmentSelectionAPI(endpoint, transport).applySaved(body))
      .review.request_uuid,
  ).toBe(ids.request);
  expect(transport.mock.calls[0]?.[1]?.body).toBe(body);
  const wrong = receipt({ ...input(), post_uuid: ids.otherPost });
  await expect(
    createAttachmentSelectionAPI(endpoint, async () =>
      Response.json(wrong),
    ).receipt(input()),
  ).rejects.toMatchObject({ code: "receipt_mismatch" });
});

it("only treats an explicit missing receipt as permission to replay", async () => {
  const missing = createAttachmentSelectionAPI(endpoint, async () =>
    Response.json({ error: "not_found" }, { status: 404 }),
  );
  expect(await missing.receipt(input())).toBeNull();
  const wrongRoute = createAttachmentSelectionAPI(endpoint, async () =>
    Response.json({ error: "route_missing" }, { status: 404 }),
  );
  await expect(wrongRoute.receipt(input())).rejects.toMatchObject({
    code: "route_missing",
  });
});

it("bounds unique manifest pagination and rejects a repeated or backward cursor", async () => {
  const item = {
    uuid: ids.manifest,
    capture_uuid: ids.capture,
    complete: false,
    declared_album: true,
    entry_count: 2,
  };
  const transport = vi.fn<typeof fetch>(async () => Response.json([item]));
  const api = createAttachmentSelectionAPI(endpoint, transport);
  expect(await api.manifests(ids.post)).toEqual([item]);
  expect(String(transport.mock.calls[0]?.[0])).toContain(
    "attachment-manifests?limit=25",
  );
  await expect(api.manifests(ids.post, ids.manifest)).rejects.toMatchObject({
    code: "invalid_response",
  });
  await expect(
    createAttachmentSelectionAPI(endpoint, async () =>
      Response.json([item, item]),
    ).manifests(ids.post),
  ).rejects.toMatchObject({ code: "invalid_response" });
});

it("keeps history scoped to one post with increasing revision cursors", async () => {
  const item = {
    uuid: ids.decision,
    post_uuid: ids.post,
    revision: 4,
    mode: "disabled",
    origin: "review",
    reason: "",
    capture_uuid: null,
    manifest_uuids: [],
    created_at: "2026-10-06T14:00:00Z",
  };
  const api = createAttachmentSelectionAPI(endpoint, async () =>
    Response.json([item]),
  );
  expect(await api.history(ids.post, 3)).toEqual([item]);
  await expect(api.history(ids.post, 4)).rejects.toMatchObject({
    code: "invalid_response",
  });
  await expect(api.history(ids.otherPost)).rejects.toMatchObject({
    code: "invalid_response",
  });
});
