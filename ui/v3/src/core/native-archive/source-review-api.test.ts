import { expect, it, vi } from "vitest";
import {
  createSourceReviewAPI,
  sourceLinkInputSchema,
} from "./source-review-api";
import {
  sourceIds,
  sourcePost,
  sourceReceipt,
} from "../../../tests/fixtures/source-review";
const endpoint = "https://example.test/stash/api/v3/archive/";

it("validates source pagination and refuses a response for another media item", async () => {
  const transport = vi
    .fn<typeof fetch>()
    .mockResolvedValue(Response.json([sourcePost()]));
  const api = createSourceReviewAPI(endpoint, transport);
  expect(await api.posts(sourceIds.media)).toEqual([sourcePost()]);
  expect(String(transport.mock.calls[0]?.[0])).toBe(
    `${endpoint}entities/${sourceIds.media}/source-posts?limit=25`,
  );
  transport.mockResolvedValue(Response.json([sourcePost(), sourcePost()]));
  await expect(api.posts(sourceIds.media)).rejects.toMatchObject({
    code: "invalid_response",
  });
  transport.mockResolvedValue(Response.json([sourcePost()]));
  await expect(
    api.posts(sourceIds.media, sourceIds.post),
  ).rejects.toMatchObject({ code: "invalid_response" });
  transport.mockResolvedValue(Response.json([sourcePost()]));
  await expect(api.posts(sourceIds.capture)).rejects.toMatchObject({
    code: "invalid_response",
  });
});
it("keeps unknown capture time distinct and validates shared revision references", async () => {
  const capture = {
    uuid: sourceIds.capture,
    post_uuid: sourceIds.post,
    revision_uuid: sourceIds.revision,
    origin: "legacy-nfo",
    platform: "reddit",
    captured_at: null,
    recorded_at: "2026-09-30T08:00:00Z",
    extractor_version: null,
  };
  const page = {
    requested_uuid: sourceIds.post,
    captures: [capture],
    revisions: [
      { uuid: sourceIds.revision, metadata: { title: "Shared title" } },
    ],
  };
  const transport = vi
    .fn<typeof fetch>()
    .mockResolvedValue(Response.json(page));
  const api = createSourceReviewAPI(endpoint, transport);
  expect(await api.captures(sourceIds.post, capture)).toEqual(page);
  const url = new URL(String(transport.mock.calls[0]?.[0]));
  expect(url.searchParams.get("after_clock")).toBe("recorded");
  expect(url.searchParams.get("after_time")).toBe(capture.recorded_at);
  transport.mockResolvedValue(Response.json({ ...page, revisions: [] }));
  await expect(api.captures(sourceIds.post)).rejects.toMatchObject({
    code: "invalid_response",
  });
  transport.mockResolvedValue(
    Response.json({
      ...page,
      captures: [{ ...capture, captured_at: capture.recorded_at }],
    }),
  );
  await expect(api.captures(sourceIds.post)).rejects.toMatchObject({
    code: "invalid_response",
  });
});

it("keeps original evidence owners while validating requested canonical review scopes", async () => {
  const row = sourcePost();
  row.association.post_uuid = sourceIds.secondCapture;
  let response: unknown = row;
  const transport = vi.fn<typeof fetch>(async () => Response.json(response));
  const api = createSourceReviewAPI(endpoint, transport);
  expect(await api.review(sourceIds.post, sourceIds.media)).toEqual(row);
  await expect(
    api.review(sourceIds.secondCapture, sourceIds.media),
  ).rejects.toMatchObject({ code: "invalid_response" });
  response = { requested_uuid: sourceIds.secondCapture, urls: row.urls };
  expect(await api.urls(sourceIds.secondCapture)).toEqual(row.urls);
  await expect(api.urls(sourceIds.post)).rejects.toMatchObject({
    code: "invalid_response",
  });
  response = {
    requested_uuid: sourceIds.secondCapture,
    urls: [...row.urls, ...row.urls],
  };
  await expect(api.urls(sourceIds.secondCapture)).rejects.toThrow();
});
it("compares every receipt guard and never treats a malformed success as permission to send again", async () => {
  const input = sourceLinkInputSchema.parse({
    uuid: sourceIds.decision,
    post_uuid: sourceIds.post,
    media_uuid: sourceIds.media,
    expected_post_revision: 4,
    expected_media_revision: 2,
    expected_decisions: [],
    state: "linked",
    origin: "review",
    reason: "Reviewed",
  });
  for (const change of [
    { state: "unlinked" },
    { post_revision: 7 },
    { media_revision: 7 },
    { reason: "Different" },
    { post_uuid: sourceIds.capture },
  ]) {
    const api = createSourceReviewAPI(
      endpoint,
      vi
        .fn<typeof fetch>()
        .mockResolvedValue(
          Response.json({ ...sourceReceipt(input), ...change }),
        ),
    );
    await expect(api.receipt(input)).rejects.toMatchObject({
      code: "mismatched_receipt",
    });
  }
});

it("pages complete version summaries and preserves unknown observation times", async () => {
  const summary = {
    uuid: sourceIds.revision,
    metadata: { title: "Shared title" },
    count: 200,
    unknown_count: 2,
    first_seen: "2026-10-01T01:00:00Z",
    last_seen: "2026-10-08T01:00:00Z",
  };
  const transport = vi
    .fn<typeof fetch>()
    .mockResolvedValue(
      Response.json({ requested_uuid: sourceIds.post, revisions: [summary] }),
    );
  const api = createSourceReviewAPI(endpoint, transport);
  expect(await api.captureHistory(sourceIds.post)).toEqual([summary]);
  expect(String(transport.mock.calls[0]?.[0])).toContain(
    "/capture-history?limit=25",
  );
  transport.mockResolvedValue(
    Response.json({ requested_uuid: sourceIds.post, revisions: [summary] }),
  );
  await expect(
    api.captureHistory(sourceIds.post, summary.uuid),
  ).rejects.toMatchObject({ code: "invalid_response" });
  transport.mockResolvedValue(
    Response.json({
      requested_uuid: sourceIds.post,
      revisions: [
        { ...summary, unknown_count: 200, first_seen: null, last_seen: null },
      ],
    }),
  );
  expect((await api.captureHistory(sourceIds.post))[0]?.first_seen).toBeNull();
  transport.mockResolvedValue(
    Response.json({
      requested_uuid: sourceIds.post,
      revisions: [{ ...summary, unknown_count: 201 }],
    }),
  );
  await expect(api.captureHistory(sourceIds.post)).rejects.toMatchObject({
    code: "invalid_response",
  });
});
