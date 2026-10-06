import { IDBFactory } from "fake-indexeddb";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createAlbumReviewAPI, type AlbumJob } from "./album-review-api";
import { createAlbumReviewOutbox } from "./album-review-outbox";
import {
  albumJob,
  albumPreview,
  albumReviewID,
  albumReviewPost,
  publishedAlbumJob,
} from "../../../tests/fixtures/album-review";

const endpoint = "https://example.test/library/api/v3/archive/";
beforeEach(() => vi.stubGlobal("indexedDB", new IDBFactory()));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
function server() {
  const jobs = new Map<string, AlbumJob>();
  const receipts = new Map<string, string>();
  const writes: string[] = [];
  const calls: RequestInit[] = [];
  let lose: "request" | "reply" | undefined;
  let conflict = "";
  let corrupt = false;
  let sequence = 8;
  const fetcher = vi.fn<typeof fetch>(async (url, options) => {
    calls.push(options ?? {});
    const path = new URL(String(url)).pathname.split("/archive/")[1]!;
    if (options?.method === "GET") {
      const id = path.startsWith("album-backfill-requests/")
        ? receipts.get(path.split("/")[1]!)
        : path.split("/")[1];
      const job = id ? jobs.get(id) : undefined;
      if (!job) return Response.json({ error: "not_found" }, { status: 404 });
      return Response.json(
        corrupt ? { ...job, post_uuid: albumReviewID(99) } : job,
      );
    }
    const body = String(options?.body);
    writes.push(body);
    if (lose === "request") {
      lose = undefined;
      throw new TypeError("Request lost");
    }
    if (conflict) return Response.json({ error: conflict }, { status: 409 });
    const input = JSON.parse(body);
    let job: AlbumJob;
    const prior = jobs.get(path.split("/")[1]!);
    if (path.endsWith("/cancel")) {
      if (!prior || prior.revision !== input.expected_revision)
        return Response.json({ error: "album_job_changed" }, { status: 409 });
      job = { ...prior, state: "cancelled", revision: prior.revision + 1 };
    } else if (path.endsWith("/retry")) {
      if (!prior || prior.revision !== input.expected_revision)
        return Response.json({ error: "album_job_changed" }, { status: 409 });
      job = albumJob({
        ...prior,
        job_uuid: albumReviewID(++sequence + 100),
        sequence,
        revision: 1,
        state: "queued",
        hooks_finished: false,
        resume_from_job_uuid: prior.job_uuid,
      });
      receipts.set(input.request_uuid, job.job_uuid);
    } else {
      job = albumJob({
        job_uuid: albumReviewID(++sequence + 100),
        sequence,
        policy: input.policy,
        signature: input.signature,
      });
      receipts.set(input.request_uuid, job.job_uuid);
    }
    jobs.set(job.job_uuid, job);
    if (lose === "reply") {
      lose = undefined;
      throw new TypeError("Reply lost");
    }
    return Response.json(job, { status: job.state === "queued" ? 202 : 200 });
  });
  return {
    api: createAlbumReviewAPI(endpoint, fetcher),
    fetcher,
    calls,
    writes,
    jobs,
    loseRequest() {
      lose = "request";
    },
    loseReply() {
      lose = "reply";
    },
    conflict(code: string) {
      conflict = code;
    },
    corrupt() {
      corrupt = true;
    },
  };
}

it("persists before send and recovers a lost admission response after reload without another POST", async () => {
  const remote = server(),
    box = createAlbumReviewOutbox(remote.api);
  remote.loseReply();
  const saved = await box.prepare(albumPreview());
  expect(remote.calls).toHaveLength(0);
  await expect(box.deliver(albumReviewPost)).rejects.toThrow("Reply lost");
  expect(await box.read(albumReviewPost)).toEqual(saved);
  const reopened = createAlbumReviewOutbox(remote.api);
  const job = await reopened.deliver(albumReviewPost);
  expect(job.state).toBe("queued");
  expect(job.publication_committed).toBe(false);
  expect(await reopened.read(albumReviewPost)).toMatchObject({
    state: "admitted",
    request_uuid: saved.request_uuid,
    job_uuid: job.job_uuid,
  });
  expect(remote.writes).toEqual([saved.body]);
  expect(await reopened.deliver(albumReviewPost)).toEqual(job);
  expect(remote.writes).toHaveLength(1);
  expect(
    remote.calls.every(
      (call) => call.credentials === "same-origin" && call.redirect === "error",
    ),
  ).toBe(true);
});
it("replays the same request bytes when the original request never arrived", async () => {
  const remote = server(),
    box = createAlbumReviewOutbox(remote.api);
  remote.loseRequest();
  const saved = await box.prepare(albumPreview());
  await expect(box.deliver(albumReviewPost)).rejects.toThrow("Request lost");
  await box.deliver(albumReviewPost);
  expect(remote.writes).toEqual([saved.body, saved.body]);
});
it("opening saved job status can never deliver a pending request", async () => {
  const remote = server(),
    box = createAlbumReviewOutbox(remote.api);
  const pending = await box.prepare(albumPreview());
  await expect(box.inspectAdmitted(pending)).rejects.toMatchObject({
    code: "invalid_saved_request",
  });
  expect(remote.calls).toHaveLength(0);
  const job = await box.deliver(albumReviewPost);
  const record = await box.read(albumReviewPost);
  expect(record?.state).toBe("admitted");
  expect(await box.inspectAdmitted(record!)).toEqual(job);
  expect(remote.writes).toHaveLength(1);
});
it("serializes matching intents across tabs, blocks competing actions and isolates deployments", async () => {
  const remote = server(),
    a = createAlbumReviewOutbox(remote.api),
    b = createAlbumReviewOutbox(remote.api);
  const [first, second] = await Promise.all([
    a.prepare(albumPreview()),
    b.prepare(albumPreview()),
  ]);
  expect(first).toEqual(second);
  await expect(
    b.prepare({ ...albumPreview(), signature: "b".repeat(64) }),
  ).rejects.toMatchObject({ code: "pending_review" });
  await expect(b.prepareCancel(albumJob())).rejects.toMatchObject({
    code: "pending_review",
  });
  await expect(
    b.forgetRejected(albumReviewPost, first.request_uuid),
  ).rejects.toMatchObject({ code: "pending_review" });
  const separate = createAlbumReviewOutbox(
    createAlbumReviewAPI(
      "https://example.test/other/api/v3/archive/",
      remote.fetcher,
    ),
  );
  expect(await separate.read(albumReviewPost)).toBeNull();
  expect(remote.calls).toHaveLength(0);
});
it("only clears a proven rejected preview, preserving ambiguous job/UUID conflicts", async () => {
  const remote = server(),
    box = createAlbumReviewOutbox(remote.api);
  const saved = await box.prepare(albumPreview());
  remote.conflict("album_preview_changed");
  await expect(box.deliver(albumReviewPost)).rejects.toMatchObject({
    code: "album_preview_changed",
  });
  expect((await box.read(albumReviewPost))?.state).toBe("rejected");
  await expect(box.prepare(albumPreview())).rejects.toMatchObject({
    code: "pending_review",
  });
  await expect(
    box.forgetRejected(albumReviewPost, albumReviewID(50)),
  ).rejects.toMatchObject({ code: "pending_review" });
  await box.forgetRejected(albumReviewPost, saved.request_uuid);
  remote.conflict("album_job_changed");
  const next = await box.prepare(albumPreview());
  await expect(box.deliver(albumReviewPost)).rejects.toMatchObject({
    code: "album_job_changed",
  });
  expect(await box.read(albumReviewPost)).toEqual(next);
});
it("recovers notification-only retries with the original committed publication", async () => {
  const remote = server(),
    box = createAlbumReviewOutbox(remote.api);
  const prior = publishedAlbumJob({ state: "failed" });
  remote.jobs.set(prior.job_uuid, prior);
  const saved = await box.prepareRetry(prior);
  remote.loseReply();
  await expect(box.deliver(albumReviewPost)).rejects.toThrow("Reply lost");
  const job = await createAlbumReviewOutbox(remote.api).deliver(
    albumReviewPost,
  );
  expect(job.resume_from_job_uuid).toBe(prior.job_uuid);
  expect(job.publication).toEqual(prior.publication);
  expect(job.hooks_finished).toBe(false);
  expect(remote.writes).toEqual([saved.body]);
});
it("rejects a resumed receipt that loses or substitutes the committed event", async () => {
  const remote = server(),
    box = createAlbumReviewOutbox(remote.api);
  const prior = publishedAlbumJob({ state: "cancelled" });
  remote.jobs.set(prior.job_uuid, prior);
  const saved = await box.prepareRetry(prior);
  remote.loseReply();
  await expect(box.deliver(albumReviewPost)).rejects.toThrow("Reply lost");
  const resumed = [...remote.jobs.values()].find(
    (job) => job.resume_from_job_uuid,
  );
  expect(resumed).toBeDefined();
  remote.jobs.set(resumed!.job_uuid, {
    ...resumed!,
    publication: undefined,
    publication_committed: false,
  });
  await expect(box.deliver(albumReviewPost)).rejects.toMatchObject({
    code: "mismatched_receipt",
  });
  expect(await box.read(albumReviewPost)).toEqual(saved);
  expect(remote.writes).toHaveLength(1);
});
it("does not send or discard an intent when the saved receipt belongs to another post", async () => {
  const remote = server(),
    box = createAlbumReviewOutbox(remote.api);
  const saved = await box.prepare(albumPreview());
  remote.loseReply();
  await expect(box.deliver(albumReviewPost)).rejects.toThrow("Reply lost");
  remote.corrupt();
  await expect(box.deliver(albumReviewPost)).rejects.toMatchObject({
    code: "invalid_response",
  });
  expect(await box.read(albumReviewPost)).toEqual(saved);
  expect(remote.writes).toHaveLength(1);
});
it("recovers cancellation from current job state and retains already committed changes", async () => {
  const remote = server(),
    box = createAlbumReviewOutbox(remote.api);
  const prior = publishedAlbumJob();
  remote.jobs.set(prior.job_uuid, prior);
  const saved = await box.prepareCancel(prior);
  remote.loseReply();
  await expect(box.deliver(albumReviewPost)).rejects.toThrow("Reply lost");
  const result = await createAlbumReviewOutbox(remote.api).deliver(
    albumReviewPost,
  );
  expect(result.state).toBe("cancelled");
  expect(result.publication).toEqual(prior.publication);
  expect(result.hooks_finished).toBe(false);
  expect(remote.writes).toEqual([saved.body]);
});
it("rejects stale cancellation before POST and allows fresh review only after recording that rejection", async () => {
  const remote = server(),
    box = createAlbumReviewOutbox(remote.api);
  const prior = albumJob();
  remote.jobs.set(prior.job_uuid, { ...prior, revision: 2, state: "running" });
  const saved = await box.prepareCancel(prior);
  await expect(box.deliver(albumReviewPost)).rejects.toMatchObject({
    code: "album_cancel_changed",
  });
  expect(remote.writes).toHaveLength(0);
  expect((await box.read(albumReviewPost))?.state).toBe("rejected");
  await box.forgetRejected(albumReviewPost, saved.request_uuid);
  expect(await box.read(albumReviewPost)).toBeNull();
});
it("concurrent cancellation recovery never applies an old revision to newer work", async () => {
  const remote = server(),
    a = createAlbumReviewOutbox(remote.api),
    b = createAlbumReviewOutbox(remote.api);
  const prior = albumJob();
  remote.jobs.set(prior.job_uuid, prior);
  const [first, second] = await Promise.all([
    a.prepareCancel(prior),
    b.prepareCancel(prior),
  ]);
  expect(first).toEqual(second);
  const results = await Promise.all([
    a.deliver(albumReviewPost),
    b.deliver(albumReviewPost),
  ]);
  expect(
    results.every((job) => job.state === "cancelled" && job.revision === 2),
  ).toBe(true);
  expect((await a.read(albumReviewPost))?.state).toBe("admitted");
});
it("requires durable storage and valid operation state before sending anything", async () => {
  const remote = server(),
    box = createAlbumReviewOutbox(remote.api);
  await expect(box.prepareRetry(albumJob())).rejects.toMatchObject({
    code: "invalid_saved_request",
  });
  await expect(
    box.prepareCancel(albumJob({ state: "failed" })),
  ).rejects.toMatchObject({ code: "invalid_saved_request" });
  await expect(
    box.prepare({
      ...albumPreview(),
      action: "disabled",
      initial_metadata: undefined,
    }),
  ).rejects.toMatchObject({ code: "album_not_applicable" });
  vi.stubGlobal("indexedDB", {
    open() {
      throw new Error("Storage unavailable");
    },
  });
  await expect(box.prepare(albumPreview())).rejects.toThrow(
    "Storage unavailable",
  );
  expect(remote.calls).toHaveLength(0);
});
