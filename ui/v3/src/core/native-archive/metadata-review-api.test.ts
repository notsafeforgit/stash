import { expect, it, vi } from "vitest";
import {
  createMetadataReviewAPI,
  editRequestKey,
  metadataReviewEndpoint,
} from "./metadata-review-api";
import { ids, preview } from "../../../tests/fixtures/metadata-review";

it("uses the application mount with same-origin session requests and passes cancellation", async () => {
  const endpoint = metadataReviewEndpoint(
    new URL("https://example.test/stash/?unrelated=secret#fragment"),
  );
  expect(endpoint).toBe("https://example.test/stash/api/v3/archive/");
  const transport = vi.fn<typeof fetch>(async () =>
    Response.json({
      uuid: ids.entity,
      revision: 2,
      kind: "scene",
      local_id: 7,
    }),
  );
  const api = createMetadataReviewAPI(endpoint, transport);
  const controller = new AbortController();
  await api.identity("scene", "7", controller.signal);
  expect(String(transport.mock.calls[0]?.[0])).toBe(
    `${endpoint}entity-identities/scene/7`,
  );
  expect(transport.mock.calls[0]?.[1]).toMatchObject({
    credentials: "same-origin",
    cache: "no-store",
    signal: controller.signal,
    redirect: "error",
  });
  expect(() =>
    metadataReviewEndpoint(new URL("https://user:secret@example.test/")),
  ).toThrow();
});

it("binds previews to their request and compares canonical selections independently of key order", async () => {
  const body = {
    ...preview().input,
    request_uuid: ids.entity,
    digest: preview().digest,
    selections: {
      A: { uuid: ids.performer, revision: 1 },
      B: { uuid: ids.otherPerformer, revision: 2 },
    },
  };
  expect(editRequestKey(body)).toBe(
    editRequestKey({
      ...body,
      selections: { B: body.selections.B, A: body.selections.A },
    }),
  );
  const api = createMetadataReviewAPI(
    "https://example.test/api/v3/archive/",
    async () =>
      Response.json({
        ...preview(),
        input: { ...preview().input, entity_uuid: ids.file },
      }),
  );
  await expect(api.preview(preview().input)).rejects.toMatchObject({
    code: "preview_mismatch",
  });
});

it("bounds pages, sends both keyset parts and does not send internal settings or retained values", async () => {
  const transport = vi.fn<typeof fetch>(async () => Response.json([]));
  const api = createMetadataReviewAPI(
    "https://example.test/api/v3/archive/",
    transport,
  );
  await api.edits(ids.entity, {
    history_uuid: ids.history,
    match_uuid: ids.match,
    collection_uuid: ids.collection,
    file_uuid: ids.file,
    source_time: "",
    relative_path: "media.mp4",
  });
  const url = new URL(String(transport.mock.calls[0]?.[0]));
  expect(Object.fromEntries(url.searchParams)).toEqual({
    limit: "25",
    after_history: ids.history,
    after_match: ids.match,
  });
  await expect(
    api.applySaved(
      JSON.stringify({
        ...preview().input,
        request_uuid: ids.entity,
        digest: preview().digest,
        settings: {},
      }),
    ),
  ).rejects.toThrow();
  expect(transport).toHaveBeenCalledTimes(1);
});
