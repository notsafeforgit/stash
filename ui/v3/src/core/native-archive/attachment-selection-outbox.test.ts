import { IDBFactory, IDBObjectStore } from "fake-indexeddb";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import {
  createAttachmentSelectionAPI,
  type SelectionApply,
  type SelectionReceipt,
} from "./attachment-selection-api";
import { createAttachmentSelectionOutbox } from "./attachment-selection-outbox";
import { withReviewRecord } from "./review-storage";
import {
  ids,
  preview,
  receipt,
} from "../../../tests/fixtures/attachment-selection";

const endpoint = "https://example.test/library/api/v3/archive/";
beforeEach(() => vi.stubGlobal("indexedDB", new IDBFactory()));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function server() {
  let committed: SelectionReceipt | null = null;
  let loseResponse = false;
  let loseBeforeCommit = false;
  let refusal = "";
  let wrongReceipt = false;
  let statusUnavailable = false;
  const bodies: string[] = [];
  const transport = vi.fn<typeof fetch>(async (url, options) => {
    const path = new URL(String(url)).pathname;
    if (path.includes("/requests/"))
      return statusUnavailable
        ? Response.json({ error: "unavailable" }, { status: 503 })
        : Response.json(committed ?? { error: "not_found" }, {
            status: committed ? 200 : 404,
          });
    if (!path.endsWith("/apply") || typeof options?.body !== "string")
      throw new Error(`Unexpected request: ${path}`);
    bodies.push(options.body);
    if (loseBeforeCommit) {
      loseBeforeCommit = false;
      throw new TypeError("Connection lost");
    }
    if (refusal) return Response.json({ error: refusal }, { status: 409 });
    const input: SelectionApply = JSON.parse(options.body);
    committed = receipt(input);
    if (loseResponse) {
      loseResponse = false;
      throw new TypeError("Response lost");
    }
    return Response.json({
      review: wrongReceipt
        ? { ...committed, request: { ...input, digest: "b".repeat(64) } }
        : committed,
      replayed: false,
    });
  });
  return {
    api: createAttachmentSelectionAPI(endpoint, transport),
    transport,
    bodies,
    loseResponse: () => {
      loseResponse = true;
    },
    loseBeforeCommit: () => {
      loseBeforeCommit = true;
    },
    refuse: (code: string) => {
      refusal = code;
    },
    wrongReceipt: () => {
      wrongReceipt = true;
    },
    statusUnavailable: () => {
      statusUnavailable = true;
    },
  };
}

it("persists before sending and recovers a committed request after reload without another POST", async () => {
  const remote = server();
  remote.loseResponse();
  const first = createAttachmentSelectionOutbox(remote.api);
  const saved = await first.prepare(ids.post, preview());
  expect(remote.transport).not.toHaveBeenCalled();
  await expect(first.deliver(ids.post)).rejects.toThrow("Response lost");
  const second = createAttachmentSelectionOutbox(remote.api);
  expect(await second.read(ids.post)).toEqual(saved);
  expect((await second.deliver(ids.post)).request_uuid).toBe(
    JSON.parse(saved.body).request_uuid,
  );
  expect(remote.bodies).toEqual([saved.body]);
  expect(await first.read(ids.post)).toBeNull();
});

it("retries the exact original body if the first request never reached the server", async () => {
  const remote = server();
  remote.loseBeforeCommit();
  const first = createAttachmentSelectionOutbox(remote.api);
  const saved = await first.prepare(ids.post, preview());
  await expect(first.deliver(ids.post)).rejects.toThrow("Connection lost");
  await createAttachmentSelectionOutbox(remote.api).deliver(ids.post);
  expect(remote.bodies).toEqual([saved.body, saved.body]);
});

it("shares equal choices across tabs, but cannot replace or discard an unresolved choice", async () => {
  const remote = server();
  const a = createAttachmentSelectionOutbox(remote.api),
    b = createAttachmentSelectionOutbox(remote.api);
  const [one, two] = await Promise.all([
    a.prepare(ids.post, preview()),
    b.prepare(ids.post, {
      ...preview(),
      input: { ...preview().input, reason: "" },
    }),
  ]);
  expect(one.body).toBe(two.body);
  await expect(b.prepare(ids.post, preview(true))).rejects.toMatchObject({
    code: "pending_review",
  });
  await expect(
    b.forgetRejected(ids.post, JSON.parse(one.body).request_uuid),
  ).rejects.toMatchObject({ code: "pending_review" });
  expect(await a.read(ids.post)).toEqual(one);
  expect(remote.transport).not.toHaveBeenCalled();
});

it("isolates post journals by post UUID and application installation", async () => {
  const remote = server();
  const box = createAttachmentSelectionOutbox(remote.api);
  await box.prepare(ids.post, preview());
  const elsewhere = createAttachmentSelectionOutbox(
    createAttachmentSelectionAPI(
      "https://example.test/another/api/v3/archive/",
    ),
  );
  expect(await elsewhere.read(ids.post)).toBeNull();
  expect(await box.read(ids.otherPost)).toBeNull();
  await expect(box.prepare(ids.otherPost, preview())).rejects.toMatchObject({
    code: "post_mismatch",
  });
  expect(remote.transport).not.toHaveBeenCalled();
});

it("keeps uncertain and mismatched responses pending instead of enabling a different write", async () => {
  const remote = server();
  const box = createAttachmentSelectionOutbox(remote.api);
  const saved = await box.prepare(ids.post, preview());
  remote.wrongReceipt();
  await expect(box.deliver(ids.post)).rejects.toMatchObject({
    code: "receipt_mismatch",
  });
  expect(await box.read(ids.post)).toEqual(saved);
  await box.deliver(ids.post);
  expect(remote.bodies).toHaveLength(1);
  expect(await box.read(ids.post)).toBeNull();
});

it.each(["request_conflict", "unrecognized_refusal"])(
  "does not release a pending request after %s",
  async (code) => {
    const remote = server();
    remote.refuse(code);
    const box = createAttachmentSelectionOutbox(remote.api);
    const saved = await box.prepare(ids.post, preview());
    await expect(box.deliver(ids.post)).rejects.toMatchObject({ code });
    expect(await box.read(ids.post)).toEqual(saved);
  },
);

it("does not POST when the request receipt cannot be checked", async () => {
  const remote = server();
  remote.statusUnavailable();
  const box = createAttachmentSelectionOutbox(remote.api);
  const saved = await box.prepare(ids.post, preview());
  await expect(box.deliver(ids.post)).rejects.toMatchObject({ status: 503 });
  expect(remote.bodies).toHaveLength(0);
  expect(await box.read(ids.post)).toEqual(saved);
});

it("only releases the exact request after a definitive stale-preview refusal", async () => {
  const remote = server();
  remote.refuse("preview_changed");
  const box = createAttachmentSelectionOutbox(remote.api);
  const saved = await box.prepare(ids.post, preview());
  await expect(box.deliver(ids.post)).rejects.toMatchObject({
    code: "preview_changed",
  });
  expect((await box.read(ids.post))?.state).toBe("rejected");
  await expect(box.deliver(ids.post)).rejects.toMatchObject({
    code: "preview_changed",
  });
  expect(remote.bodies).toHaveLength(1);
  await expect(box.forgetRejected(ids.post, ids.request)).rejects.toMatchObject(
    { code: "pending_review" },
  );
  await box.forgetRejected(ids.post, JSON.parse(saved.body).request_uuid);
  expect(await box.read(ids.post)).toBeNull();
});

it("cannot send a journal row stored under the wrong post", async () => {
  const remote = server();
  const box = createAttachmentSelectionOutbox(remote.api);
  const saved = await box.prepare(ids.post, preview());
  await withReviewRecord(
    `stash-attachment-selection-review:v1:${endpoint}`,
    ids.otherPost,
    "readwrite",
    (_value, store) => store.add(saved, ids.otherPost),
  );
  await expect(box.deliver(ids.otherPost)).rejects.toMatchObject({
    code: "invalid_saved_request",
  });
  expect(remote.transport).not.toHaveBeenCalled();
});

it("aborts before transmission if saving the request fails", async () => {
  const remote = server();
  const box = createAttachmentSelectionOutbox(remote.api);
  vi.spyOn(IDBObjectStore.prototype, "add").mockImplementationOnce(() => {
    throw new DOMException("Quota exceeded", "QuotaExceededError");
  });
  await expect(box.prepare(ids.post, preview())).rejects.toThrow(
    "Quota exceeded",
  );
  expect(await box.read(ids.post)).toBeNull();
  expect(remote.transport).not.toHaveBeenCalled();
});

it("a late confirmation cannot erase a new choice saved in another tab", async () => {
  let finish: (response: Response) => void = () => {
    throw new Error("No pending request");
  };
  let started: () => void = () => {};
  const sending = new Promise<void>((resolve) => {
    started = resolve;
  });
  let first = true;
  const api = createAttachmentSelectionAPI(endpoint, async (_url, options) => {
    if (!options?.body)
      return Response.json({ error: "not_found" }, { status: 404 });
    const review = receipt(JSON.parse(String(options.body)));
    if (first) {
      first = false;
      return new Promise<Response>((resolve) => {
        finish = resolve;
        started();
      });
    }
    return Response.json({ review, replayed: true });
  });
  const a = createAttachmentSelectionOutbox(api),
    b = createAttachmentSelectionOutbox(api);
  const original = await a.prepare(ids.post, preview());
  const late = a.deliver(ids.post);
  await sending;
  await b.deliver(ids.post);
  const next = await b.prepare(ids.post, preview(true));
  finish(
    Response.json({
      review: receipt(JSON.parse(original.body)),
      replayed: false,
    }),
  );
  await late;
  expect(await a.read(ids.post)).toEqual(next);
});
