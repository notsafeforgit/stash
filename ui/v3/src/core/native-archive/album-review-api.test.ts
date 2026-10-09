import { expect, it, vi } from "vitest";
import { postIdentity, postIds } from "../../../tests/fixtures/source-posts";
import {
  albumJobSchema,
  albumPreviewSchema,
  createAlbumReviewAPI,
} from "./album-review-api";
import {
  albumJob,
  albumPreview,
  albumReviewID,
  albumReviewPost,
  publishedAlbumJob,
} from "../../../tests/fixtures/album-review";

const endpoint = "https://example.test/library/api/v3/archive/";
it("resolves the current post for new work without rewriting an original job", async () => {
  const identity = postIdentity(postIds.post, postIds.otherPost);
  const transport = vi.fn<typeof fetch>(async () => Response.json(identity));
  const api = createAlbumReviewAPI(endpoint, transport);
  expect(await api.post(postIds.post)).toEqual(identity.canonical);
  expect(String(transport.mock.calls[0]?.[0])).toBe(
    `${endpoint}posts/${postIds.post}/identity`,
  );
  await expect(api.post(postIds.otherPost)).rejects.toMatchObject({
    code: "invalid_response",
  });
});
it("previews one post with an explicit policy through the public session endpoint", async () => {
  const fetcher = vi.fn<typeof fetch>(async () =>
    Response.json(albumPreview()),
  );
  const api = createAlbumReviewAPI(endpoint, fetcher);
  const abort = new AbortController();
  expect(
    await api.preview(albumReviewPost, "source-identifiers-v1", abort.signal),
  ).toEqual(albumPreview());
  expect(String(fetcher.mock.calls[0]?.[0])).toBe(
    `${endpoint}posts/${albumReviewPost}/album-backfill/preview`,
  );
  expect(fetcher.mock.calls[0]?.[1]).toMatchObject({
    method: "POST",
    credentials: "same-origin",
    redirect: "error",
    cache: "no-store",
    body: '{"policy":"source-identifiers-v1"}',
    signal: abort.signal,
  });
});
it("rejects crossed preview identities and policies", async () => {
  let result = { ...albumPreview(), post_uuid: albumReviewID(999) };
  const api = createAlbumReviewAPI(endpoint, async () => Response.json(result));
  await expect(
    api.preview(albumReviewPost, "source-identifiers-v1"),
  ).rejects.toMatchObject({ code: "invalid_response" });
  result = { ...albumPreview(), policy: "legacy-reddit-filename-v1" };
  await expect(
    api.preview(albumReviewPost, "source-identifiers-v1"),
  ).rejects.toMatchObject({ code: "invalid_response" });
});
it("accepts partial Twitter filename recovery with its original evidence and gaps", async () => {
  const result = albumPreview();
  result.policy = "legacy-twitter-filename-v1";
  result.matches[0]!.reference = {
    namespace: "legacy:twitter:filename",
    value: "1575550205214134278_3",
  };
  result.matches[0]!.candidates[0]!.proofs[0]!.basis =
    "legacy-twitter-filename";
  result.entries.forEach((entry, index) => {
    entry.position = index * 2 + 2;
  });
  const api = createAlbumReviewAPI(endpoint, async () => Response.json(result));
  expect(await api.preview(albumReviewPost, result.policy)).toEqual(result);
});
it("keeps repeated attachments and gaps but rejects duplicate positions, partial candidates and overlapping changes", () => {
  const preview = albumPreview();
  preview.entries.push({ ...preview.entries[0]!, position: 99 });
  expect(albumPreviewSchema.safeParse(preview).success).toBe(true);
  expect(
    albumPreviewSchema.safeParse({
      ...preview,
      entries: [...preview.entries, preview.entries[0]],
    }).success,
  ).toBe(false);
  expect(
    albumPreviewSchema.safeParse({ ...preview, remove: [preview.add[0]] })
      .success,
  ).toBe(false);
  expect(
    albumPreviewSchema.safeParse({
      ...preview,
      matches: [{ ...preview.matches[0], candidates: [] }],
    }).success,
  ).toBe(false);
  expect(
    albumPreviewSchema.safeParse({
      ...preview,
      matches: [{ ...preview.matches[0], attachment_uuid: albumReviewID(999) }],
    }).success,
  ).toBe(false);
  expect(
    albumPreviewSchema.safeParse({ ...preview, initial_metadata: undefined })
      .success,
  ).toBe(false);
});
it("distinguishes committed changes from finished notifications even after cancellation", () => {
  expect(albumJobSchema.safeParse(publishedAlbumJob()).success).toBe(true);
  expect(
    albumJobSchema.safeParse(publishedAlbumJob({ state: "cancelled" })).success,
  ).toBe(true);
  expect(
    albumJobSchema.safeParse(
      albumJob({ state: "succeeded", hooks_finished: true }),
    ).success,
  ).toBe(false);
  expect(
    albumJobSchema.safeParse(
      publishedAlbumJob({ state: "succeeded", hooks_finished: true }),
    ).success,
  ).toBe(true);
  expect(
    albumJobSchema.safeParse(
      publishedAlbumJob({ publication_committed: false }),
    ).success,
  ).toBe(false);
  expect(
    albumJobSchema.safeParse(publishedAlbumJob({ job_uuid: albumReviewID(99) }))
      .success,
  ).toBe(false);
  expect(
    albumJobSchema.safeParse(
      publishedAlbumJob({
        job_uuid: albumReviewID(99),
        resume_from_job_uuid: albumReviewID(20),
      }),
    ).success,
  ).toBe(true);
});
it("only treats the defined missing receipt response as absent", async () => {
  let status = 404,
    error = "not_found";
  const api = createAlbumReviewAPI(endpoint, async () =>
    Response.json({ error }, { status }),
  );
  expect(await api.receipt(albumReviewID(50))).toBeNull();
  error = "route_missing";
  await expect(api.receipt(albumReviewID(50))).rejects.toMatchObject({
    code: "route_missing",
  });
  status = 403;
  await expect(api.receipt(albumReviewID(50))).rejects.toMatchObject({
    status: 403,
  });
});
it("validates exact saved submit scope, job paths, retry parents and cancellation responses", async () => {
  let result = albumJob();
  const calls: RequestInit[] = [];
  const api = createAlbumReviewAPI(endpoint, async (_url, options) => {
    calls.push(options ?? {});
    return Response.json(result);
  });
  const body = JSON.stringify({
    request_uuid: albumReviewID(50),
    policy: result.policy,
    signature: result.signature,
  });
  expect(await api.submitSaved(albumReviewPost, body)).toEqual(result);
  expect(calls[0]?.body).toBe(body);
  await expect(api.job(albumReviewID(21))).rejects.toMatchObject({
    code: "invalid_response",
  });
  result = { ...result, signature: "b".repeat(64) };
  await expect(api.submitSaved(albumReviewPost, body)).rejects.toMatchObject({
    code: "invalid_response",
  });
  const previous = albumJob({ state: "failed", revision: 3 });
  result = albumJob({
    job_uuid: albumReviewID(21),
    sequence: 8,
    resume_from_job_uuid: albumReviewID(22),
  });
  await expect(
    api.retrySaved(
      previous,
      JSON.stringify({ request_uuid: albumReviewID(50), expected_revision: 3 }),
    ),
  ).rejects.toMatchObject({ code: "invalid_response" });
  result = albumJob();
  await expect(
    api.cancelSaved(result, '{"expected_revision":1}'),
  ).rejects.toMatchObject({ code: "invalid_response" });
});
it("requires increasing scoped history and attempt cursors", async () => {
  let result: unknown = [
    albumJob(),
    albumJob({ job_uuid: albumReviewID(21), sequence: 8 }),
  ];
  const fetcher = vi.fn<typeof fetch>(async () => Response.json(result));
  const api = createAlbumReviewAPI(endpoint, fetcher);
  expect(await api.history(albumReviewPost, 6)).toHaveLength(2);
  expect(String(fetcher.mock.calls[0]?.[0])).toContain("limit=25&after=6");
  await expect(api.history(albumReviewPost, 7)).rejects.toMatchObject({
    code: "invalid_response",
  });
  result = [albumJob({ post_uuid: albumReviewID(99) })];
  await expect(api.history(albumReviewPost)).rejects.toMatchObject({
    code: "invalid_response",
  });
  result = [
    {
      job_uuid: albumReviewID(20),
      fence: 1,
      owner_uuid: albumReviewID(40),
      started_at: "2026-10-06T12:00:00Z",
      outcome: "running",
      result: {},
      error_code: "",
    },
  ];
  expect(await api.attempts(albumReviewID(20))).toHaveLength(1);
  await expect(api.attempts(albumReviewID(20), 1)).rejects.toMatchObject({
    code: "invalid_response",
  });
  await expect(api.attempts(albumReviewID(21))).rejects.toMatchObject({
    code: "invalid_response",
  });
});
