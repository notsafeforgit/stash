import { IDBFactory, IDBObjectStore } from "fake-indexeddb";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import {
  createAccountReviewAPI,
  type OwnershipApply,
  type OwnershipReceipt,
} from "./account-review-api";
import { createAccountReviewOutbox } from "./account-review-outbox";
import { withReviewRecord } from "./review-storage";
import { ids, preview, receipt } from "../../../tests/fixtures/account-review";

const endpoint = "https://example.test/library/api/v3/archive/";
beforeEach(() => vi.stubGlobal("indexedDB", new IDBFactory()));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function server() {
  let committed: OwnershipReceipt | null = null;
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
    const input: OwnershipApply = JSON.parse(options.body);
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
    api: createAccountReviewAPI(endpoint, transport),
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
  const first = createAccountReviewOutbox(remote.api);
  const saved = await first.prepare(ids.account, preview());
  expect(remote.transport).not.toHaveBeenCalled();
  await expect(first.deliver(ids.account)).rejects.toThrow("Response lost");
  const second = createAccountReviewOutbox(remote.api);
  expect(await second.read(ids.account)).toEqual(saved);
  expect((await second.deliver(ids.account)).request_uuid).toBe(
    JSON.parse(saved.body).request_uuid,
  );
  expect(remote.bodies).toEqual([saved.body]);
  expect(await first.read(ids.account)).toBeNull();
});

it("retries the exact original body if the first request never reached the server", async () => {
  const remote = server();
  remote.loseBeforeCommit();
  const first = createAccountReviewOutbox(remote.api);
  const saved = await first.prepare(ids.account, preview());
  await expect(first.deliver(ids.account)).rejects.toThrow("Connection lost");
  await createAccountReviewOutbox(remote.api).deliver(ids.account);
  expect(remote.bodies).toEqual([saved.body, saved.body]);
});

it("shares equal choices across tabs, but cannot replace or discard an unresolved choice", async () => {
  const remote = server();
  const a = createAccountReviewOutbox(remote.api),
    b = createAccountReviewOutbox(remote.api);
  const [one, two] = await Promise.all([
    a.prepare(ids.account, preview()),
    b.prepare(ids.account, {
      ...preview(),
      input: { ...preview().input, reason: "" },
    }),
  ]);
  expect(one.body).toBe(two.body);
  await expect(b.prepare(ids.account, preview(true))).rejects.toMatchObject({
    code: "pending_review",
  });
  await expect(
    b.forgetRejected(ids.account, JSON.parse(one.body).request_uuid),
  ).rejects.toMatchObject({ code: "pending_review" });
  expect(await a.read(ids.account)).toEqual(one);
  expect(remote.transport).not.toHaveBeenCalled();
});

it("isolates account journals by account UUID and application installation", async () => {
  const remote = server();
  const box = createAccountReviewOutbox(remote.api);
  await box.prepare(ids.account, preview());
  const elsewhere = createAccountReviewOutbox(
    createAccountReviewAPI("https://example.test/another/api/v3/archive/"),
  );
  expect(await elsewhere.read(ids.account)).toBeNull();
  expect(await box.read(ids.otherAccount)).toBeNull();
  await expect(box.prepare(ids.otherAccount, preview())).rejects.toMatchObject({
    code: "account_mismatch",
  });
  expect(remote.transport).not.toHaveBeenCalled();
});

it("keeps uncertain and mismatched responses pending instead of enabling a different write", async () => {
  const remote = server();
  const box = createAccountReviewOutbox(remote.api);
  const saved = await box.prepare(ids.account, preview());
  remote.wrongReceipt();
  await expect(box.deliver(ids.account)).rejects.toMatchObject({
    code: "receipt_mismatch",
  });
  expect(await box.read(ids.account)).toEqual(saved);
  await box.deliver(ids.account);
  expect(remote.bodies).toHaveLength(1);
  expect(await box.read(ids.account)).toBeNull();
});

it.each(["request_conflict", "unrecognized_refusal"])(
  "does not release a pending request after %s",
  async (code) => {
    const remote = server();
    remote.refuse(code);
    const box = createAccountReviewOutbox(remote.api);
    const saved = await box.prepare(ids.account, preview());
    await expect(box.deliver(ids.account)).rejects.toMatchObject({ code });
    expect(await box.read(ids.account)).toEqual(saved);
  },
);

it("does not POST when the request receipt cannot be checked", async () => {
  const remote = server();
  remote.statusUnavailable();
  const box = createAccountReviewOutbox(remote.api);
  const saved = await box.prepare(ids.account, preview());
  await expect(box.deliver(ids.account)).rejects.toMatchObject({ status: 503 });
  expect(remote.bodies).toHaveLength(0);
  expect(await box.read(ids.account)).toEqual(saved);
});

it("only releases the exact request after a definitive stale-preview refusal", async () => {
  const remote = server();
  remote.refuse("preview_changed");
  const box = createAccountReviewOutbox(remote.api);
  const saved = await box.prepare(ids.account, preview());
  await expect(box.deliver(ids.account)).rejects.toMatchObject({
    code: "preview_changed",
  });
  expect((await box.read(ids.account))?.state).toBe("rejected");
  await expect(box.deliver(ids.account)).rejects.toMatchObject({
    code: "preview_changed",
  });
  expect(remote.bodies).toHaveLength(1);
  await expect(
    box.forgetRejected(ids.account, ids.request),
  ).rejects.toMatchObject({ code: "pending_review" });
  await box.forgetRejected(ids.account, JSON.parse(saved.body).request_uuid);
  expect(await box.read(ids.account)).toBeNull();
});

it("cannot send a journal row stored under the wrong account", async () => {
  const remote = server();
  const box = createAccountReviewOutbox(remote.api);
  const saved = await box.prepare(ids.account, preview());
  await withReviewRecord(
    `stash-account-review:v1:${endpoint}`,
    ids.otherAccount,
    "readwrite",
    (_value, store) => store.add(saved, ids.otherAccount),
  );
  await expect(box.deliver(ids.otherAccount)).rejects.toMatchObject({
    code: "invalid_saved_request",
  });
  expect(remote.transport).not.toHaveBeenCalled();
});

it("aborts before transmission if saving the request fails", async () => {
  const remote = server();
  const box = createAccountReviewOutbox(remote.api);
  vi.spyOn(IDBObjectStore.prototype, "add").mockImplementationOnce(() => {
    throw new DOMException("Quota exceeded", "QuotaExceededError");
  });
  await expect(box.prepare(ids.account, preview())).rejects.toThrow(
    "Quota exceeded",
  );
  expect(await box.read(ids.account)).toBeNull();
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
  const api = createAccountReviewAPI(endpoint, async (_url, options) => {
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
  const a = createAccountReviewOutbox(api),
    b = createAccountReviewOutbox(api);
  const original = await a.prepare(ids.account, preview());
  const late = a.deliver(ids.account);
  await sending;
  await b.deliver(ids.account);
  const next = await b.prepare(ids.account, preview(true));
  finish(
    Response.json({
      review: receipt(JSON.parse(original.body)),
      replayed: false,
    }),
  );
  await late;
  expect(await a.read(ids.account)).toEqual(next);
});
