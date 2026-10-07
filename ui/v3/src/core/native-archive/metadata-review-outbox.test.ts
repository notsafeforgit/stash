import { IDBFactory, IDBObjectStore } from "fake-indexeddb";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import {
  createMetadataReviewAPI,
  NativeArchiveError,
  type EditApply,
  type EditReceipt,
} from "./metadata-review-api";
import { createMetadataReviewOutbox } from "./metadata-review-outbox";
import { ids, preview, receipt } from "../../../tests/fixtures/metadata-review";

const target = { kind: "scene" as const, localId: "7" };
const endpoint = "https://example.test/library/api/v3/archive/";
beforeEach(() => vi.stubGlobal("indexedDB", new IDBFactory()));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function server() {
  let committed: EditReceipt | null = null;
  let loseResponse = false;
  let loseBeforeCommit = false;
  let stale = false;
  let wrongReceipt = false;
  const bodies: string[] = [];
  const requests: { path: string; options?: RequestInit }[] = [];
  const transport: typeof fetch = vi.fn(async (url, options) => {
    const path = new URL(String(url)).pathname;
    requests.push({ path, options });
    if (path.includes("/requests/"))
      return Response.json(committed ?? { error: "not_found" }, {
        status: committed ? 200 : 404,
      });
    if (!path.endsWith("/apply") || typeof options?.body !== "string")
      throw new Error(`Unexpected request: ${path}`);
    bodies.push(options.body);
    if (loseBeforeCommit) {
      loseBeforeCommit = false;
      throw new TypeError("Connection lost");
    }
    if (stale)
      return Response.json({ error: "preview_changed" }, { status: 409 });
    const request: EditApply = JSON.parse(options.body);
    committed = receipt(request);
    if (loseResponse) {
      loseResponse = false;
      throw new TypeError("Response lost");
    }
    return Response.json({
      review: wrongReceipt
        ? { ...committed, request: { ...request, digest: "b".repeat(64) } }
        : committed,
      replayed: false,
    });
  });
  return {
    api: createMetadataReviewAPI(endpoint, transport),
    bodies,
    requests,
    setLoseResponse: () => {
      loseResponse = true;
    },
    setLoseBeforeCommit: () => {
      loseBeforeCommit = true;
    },
    setStale: () => {
      stale = true;
    },
    setWrongReceipt: () => {
      wrongReceipt = true;
    },
  };
}

it("saves before send and recovers a lost committed response through a new client without another POST", async () => {
  const remote = server();
  remote.setLoseResponse();
  const first = createMetadataReviewOutbox(remote.api);
  const saved = await first.prepare(target, preview());
  expect(remote.requests).toHaveLength(0);
  await expect(first.deliver(target)).rejects.toThrow("Response lost");
  const second = createMetadataReviewOutbox(remote.api);
  expect(await second.read(target)).toEqual(saved);
  expect((await second.deliver(target)).request_uuid).toBe(
    JSON.parse(saved.body).request_uuid,
  );
  expect(remote.bodies).toEqual([saved.body]);
  expect(await first.read(target)).toBeNull();
});

it.each(["ready", "unresolved_names", "unsupported"] as const)(
  "recovers an explicit keep choice for a %s field without applying it or repeating the POST",
  async (status) => {
    const remote = server();
    remote.setLoseResponse();
    const box = createMetadataReviewOutbox(remote.api);
    const reviewed = { ...preview(), status };
    const saved = await box.prepare(target, reviewed, true);
    expect(JSON.parse(saved.body).keep_current).toBe(true);
    expect(remote.requests).toHaveLength(0);
    if (status === "ready")
      await expect(box.prepare(target, reviewed)).rejects.toMatchObject({
        code: "pending_review",
      });
    await expect(box.deliver(target)).rejects.toThrow("Response lost");
    const reopened = createMetadataReviewOutbox(remote.api);
    expect(await reopened.read(target)).toEqual(saved);
    const recovered = await reopened.deliver(target);
    expect(recovered.kept_current).toBe(true);
    expect(recovered.decision_uuid).toBeUndefined();
    expect(remote.bodies).toEqual([saved.body]);
    expect(await reopened.read(target)).toBeNull();
  },
);

it("does not replace a saved Apply request with a Keep choice", async () => {
  const remote = server();
  const box = createMetadataReviewOutbox(remote.api);
  const saved = await box.prepare(target, preview());
  expect(JSON.parse(saved.body)).not.toHaveProperty("keep_current");
  await expect(box.prepare(target, preview(), true)).rejects.toMatchObject({
    code: "pending_review",
  });
  expect(await createMetadataReviewOutbox(remote.api).read(target)).toEqual(
    saved,
  );
  expect(remote.requests).toHaveLength(0);
});

it("retries the byte-identical saved body when the first request never committed", async () => {
  const remote = server();
  remote.setLoseBeforeCommit();
  const box = createMetadataReviewOutbox(remote.api);
  const saved = await box.prepare(target, preview());
  await expect(box.deliver(target)).rejects.toThrow("Connection lost");
  await createMetadataReviewOutbox(remote.api).deliver(target);
  expect(remote.bodies).toEqual([saved.body, saved.body]);
});

it("serializes simultaneous tabs, reuses equal previews and refuses to replace an unresolved choice", async () => {
  const remote = server();
  const a = createMetadataReviewOutbox(remote.api),
    b = createMetadataReviewOutbox(remote.api);
  const [one, two] = await Promise.all([
    a.prepare(target, preview()),
    b.prepare(target, {
      ...preview(),
      input: { ...preview().input, selections: {} },
    }),
  ]);
  expect(one.body).toBe(two.body);
  await expect(
    b.prepare(target, { ...preview(), digest: "b".repeat(64) }),
  ).rejects.toMatchObject({ code: "pending_review" });
  await expect(
    b.forgetRejected(target, JSON.parse(one.body).request_uuid),
  ).rejects.toMatchObject({ code: "pending_review" });
  expect(remote.requests).toHaveLength(0);
  expect(await a.read(target)).toEqual(one);
});

it("does not send when storage fails or the preview is unresolved", async () => {
  const remote = server();
  const box = createMetadataReviewOutbox(remote.api);
  await expect(
    box.prepare(target, { ...preview(), status: "unresolved_names" }),
  ).rejects.toMatchObject({ code: "preview_not_ready" });
  vi.stubGlobal("indexedDB", {
    open() {
      throw new Error("Quota exceeded");
    },
  });
  await expect(box.prepare(target, preview())).rejects.toThrow(
    "Quota exceeded",
  );
  await expect(box.deliver(target)).rejects.toThrow("Quota exceeded");
  expect(remote.requests).toHaveLength(0);
});

it("keeps a mismatched receipt pending and recovers the actual receipt", async () => {
  const remote = server();
  remote.setWrongReceipt();
  const box = createMetadataReviewOutbox(remote.api);
  const saved = await box.prepare(target, preview());
  await expect(box.deliver(target)).rejects.toMatchObject({
    code: "receipt_mismatch",
  });
  expect(await box.read(target)).toEqual(saved);
  await box.deliver(target);
  expect(remote.bodies).toHaveLength(1);
  expect(await box.read(target)).toBeNull();
});

it("only a definitive stale-preview refusal enables discarding a saved request", async () => {
  const remote = server();
  remote.setStale();
  const box = createMetadataReviewOutbox(remote.api);
  const saved = await box.prepare(target, preview());
  await expect(box.deliver(target)).rejects.toBeInstanceOf(NativeArchiveError);
  expect((await box.read(target))?.state).toBe("rejected");
  await expect(box.forgetRejected(target, ids.entity)).rejects.toMatchObject({
    code: "pending_review",
  });
  await box.forgetRejected(target, JSON.parse(saved.body).request_uuid);
  expect(await box.read(target)).toBeNull();
});

it("isolates installations, entities and image/scene local IDs", async () => {
  const remote = server();
  const box = createMetadataReviewOutbox(remote.api);
  await box.prepare(target, preview());
  const elsewhere = createMetadataReviewOutbox(
    createMetadataReviewAPI("https://example.test/another/api/v3/archive/"),
  );
  expect(await elsewhere.read(target)).toBeNull();
  expect(await box.read({ kind: "image", localId: "7" })).toBeNull();
  expect(await box.read({ kind: "scene", localId: "8" })).toBeNull();
  expect(remote.requests).toHaveLength(0);
});

it("a failed storage write aborts before transmission and leaves no pending row", async () => {
  const remote = server();
  const box = createMetadataReviewOutbox(remote.api);
  vi.spyOn(IDBObjectStore.prototype, "add").mockImplementationOnce(() => {
    throw new DOMException("Quota exceeded", "QuotaExceededError");
  });
  await expect(box.prepare(target, preview())).rejects.toThrow(
    "Quota exceeded",
  );
  expect(await box.read(target)).toBeNull();
  expect(remote.requests).toHaveLength(0);
});

it("can prepare a secure random request on LAN HTTP without crypto.randomUUID", async () => {
  const getRandomValues = crypto.getRandomValues.bind(crypto);
  vi.stubGlobal("crypto", { getRandomValues });
  const remote = server();
  const saved = await createMetadataReviewOutbox(remote.api).prepare(
    target,
    preview(),
  );
  expect(JSON.parse(saved.body).request_uuid).toMatch(
    /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
  );
  expect(remote.requests).toHaveLength(0);
});
