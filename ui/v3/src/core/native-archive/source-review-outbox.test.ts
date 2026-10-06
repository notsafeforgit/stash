import { IDBFactory } from "fake-indexeddb";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import {
  createSourceReviewAPI,
  type SourceDecision,
  type SourceLinkInput,
} from "./source-review-api";
import { createSourceReviewOutbox } from "./source-review-outbox";
import {
  sourceIds,
  sourcePost,
  sourceReceipt,
} from "../../../tests/fixtures/source-review";

const endpoint = "https://example.test/library/api/v3/archive/";
const target = { kind: "scene" as const, localId: "7" };
beforeEach(() => vi.stubGlobal("indexedDB", new IDBFactory()));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
function server() {
  let receipt: SourceDecision | null = null;
  let loseReply = false;
  let loseRequest = false;
  let conflict = "";
  let corrupt = false;
  const writes: string[] = [];
  const requests: RequestInit[] = [];
  const fetcher: typeof fetch = vi.fn(async (url, options) => {
    requests.push(options ?? {});
    if (String(url).includes("/post-media-decisions/"))
      return Response.json(receipt, { status: receipt ? 200 : 404 });
    expect(options?.method).toBe("PUT");
    const body = String(options?.body);
    writes.push(body);
    if (loseRequest) {
      loseRequest = false;
      throw new TypeError("Request lost");
    }
    if (conflict) return Response.json({ error: conflict }, { status: 409 });
    const input: SourceLinkInput = JSON.parse(body);
    receipt = sourceReceipt(input);
    if (corrupt) receipt = { ...receipt, media_uuid: sourceIds.post };
    if (loseReply) {
      loseReply = false;
      throw new TypeError("Reply lost");
    }
    return Response.json(receipt);
  });
  return {
    api: createSourceReviewAPI(endpoint, fetcher),
    fetcher,
    writes,
    requests,
    loseReply() {
      loseReply = true;
    },
    loseRequest() {
      loseRequest = true;
    },
    conflict(code: string) {
      conflict = code;
    },
    corrupt() {
      corrupt = true;
    },
  };
}
it("persists a reviewed choice before send and recovers a committed response after reload without another PUT", async () => {
  const remote = server();
  remote.loseReply();
  const box = createSourceReviewOutbox(remote.api);
  const saved = await box.prepare(
    target,
    sourcePost().association,
    "linked",
    "Reviewed source URL",
  );
  expect(remote.requests).toHaveLength(0);
  await expect(box.deliver(target)).rejects.toThrow("Reply lost");
  expect(await box.read(target)).toEqual(saved);
  const reopened = createSourceReviewOutbox(remote.api);
  expect((await reopened.deliver(target)).uuid).toBe(
    JSON.parse(saved.body).uuid,
  );
  expect(remote.writes).toEqual([saved.body]);
  expect(await box.read(target)).toBeNull();
  expect(
    remote.requests.every(
      (request) =>
        request.credentials === "same-origin" && request.redirect === "error",
    ),
  ).toBe(true);
});
it("resends identical bytes if nothing committed and keeps all merged decisions in the guard", async () => {
  const remote = server();
  remote.loseRequest();
  const box = createSourceReviewOutbox(remote.api);
  const a = sourcePost().association;
  a.state = "conflict";
  a.decisions = [
    {
      uuid: sourceIds.decision,
      post_uuid: a.post_uuid,
      media_uuid: sourceIds.capture,
      post_revision: 3,
      media_revision: 1,
      state: "unlinked",
      origin: "review",
      reason: "Old link",
      created_at: "2026-09-30T00:00:00Z",
    },
  ];
  const saved = await box.prepare(target, a, "undecided", "Use attachments");
  expect(JSON.parse(saved.body).expected_decisions).toEqual([
    sourceIds.decision,
  ]);
  await expect(box.deliver(target)).rejects.toThrow("Request lost");
  await box.deliver(target);
  expect(remote.writes).toEqual([saved.body, saved.body]);
});
it("serializes tabs and isolates public deployment prefixes", async () => {
  const remote = server();
  const a = createSourceReviewOutbox(remote.api),
    b = createSourceReviewOutbox(remote.api);
  const [one, two] = await Promise.all([
    a.prepare(target, sourcePost().association, "linked", ""),
    b.prepare(target, sourcePost().association, "linked", ""),
  ]);
  expect(one).toEqual(two);
  await expect(
    b.prepare(target, sourcePost().association, "unlinked", ""),
  ).rejects.toMatchObject({ code: "pending_review" });
  await expect(
    b.forgetRejected(target, JSON.parse(one.body).uuid),
  ).rejects.toMatchObject({ code: "pending_review" });
  const separate = createSourceReviewOutbox(
    createSourceReviewAPI(
      "https://example.test/other/api/v3/archive/",
      remote.fetcher,
    ),
  );
  expect(await separate.read(target)).toBeNull();
  expect(remote.requests).toHaveLength(0);
});
it("only forgets a proven rejected guard, never an ambiguous UUID conflict", async () => {
  const remote = server();
  remote.conflict("post_media_conflict");
  const box = createSourceReviewOutbox(remote.api);
  const saved = await box.prepare(
    target,
    sourcePost().association,
    "linked",
    "",
  );
  await expect(box.deliver(target)).rejects.toMatchObject({
    code: "post_media_conflict",
  });
  expect((await box.read(target))?.state).toBe("rejected");
  await expect(box.deliver(target)).rejects.toMatchObject({
    code: "post_media_conflict",
  });
  expect(remote.writes).toHaveLength(1);
  await box.forgetRejected(target, JSON.parse(saved.body).uuid);
  remote.conflict("post_media_request_conflict");
  const next = await box.prepare(
    target,
    sourcePost().association,
    "linked",
    "",
  );
  await expect(box.deliver(target)).rejects.toMatchObject({
    code: "post_media_request_conflict",
  });
  expect(await box.read(target)).toEqual(next);
});
it("does not discard a mismatched receipt or write if durable storage is unavailable", async () => {
  const remote = server();
  remote.corrupt();
  const box = createSourceReviewOutbox(remote.api);
  const saved = await box.prepare(
    target,
    sourcePost().association,
    "linked",
    "",
  );
  await expect(box.deliver(target)).rejects.toMatchObject({
    code: "mismatched_receipt",
  });
  expect(await box.read(target)).toEqual(saved);
  await expect(box.deliver(target)).rejects.toMatchObject({
    code: "mismatched_receipt",
  });
  expect(remote.writes).toHaveLength(1);
  vi.stubGlobal("indexedDB", {
    open() {
      throw new Error("Storage unavailable");
    },
  });
  await expect(
    box.prepare(
      { ...target, localId: "8" },
      sourcePost().association,
      "linked",
      "",
    ),
  ).rejects.toThrow("Storage unavailable");
  expect(remote.writes).toHaveLength(1);
});
it("rejects retired sources and invalid Unicode byte lengths before saving", async () => {
  const box = createSourceReviewOutbox(server().api);
  await expect(
    box.prepare(
      target,
      { ...sourcePost().association, post_state: "forgotten" },
      "linked",
      "",
    ),
  ).rejects.toMatchObject({ code: "source_unavailable" });
  await expect(
    box.prepare(target, sourcePost().association, "linked", "花".repeat(1400)),
  ).rejects.toThrow();
  expect(await box.read(target)).toBeNull();
});
