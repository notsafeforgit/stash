import { IDBFactory, IDBKeyRange } from "fake-indexeddb";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { createSavedActionsAPI } from "./saved-actions";
import {
  createAccountReviewAPI,
  type OwnershipApply,
  type OwnershipReceipt,
} from "./account-review-api";
import { createAccountReviewOutbox } from "./account-review-outbox";
import { withReviewRecord } from "./review-storage";
import { ids, preview, receipt } from "../../../tests/fixtures/account-review";
import { createCollectionAPI } from "./collection-api";
import { createCollectionOutbox } from "./collection-outbox";
import {
  collectionInput,
  collectionID,
} from "../../../tests/fixtures/collections";
import { mediaPreview } from "../../../tests/fixtures/source-association";
import { mergeReceipt } from "../../../tests/fixtures/post-consolidation";
import { createManualIntakeAPI } from "./manual-intake-api";
import { createManualIntakeOutbox } from "./manual-intake-outbox";
import { manualPreview } from "../../../tests/fixtures/manual-intake";

const endpoint = "https://example.test/library/api/v3/archive/";
const accountDatabase = `stash-account-review:v1:${endpoint}`;
beforeEach(() => {
  vi.stubGlobal("indexedDB", new IDBFactory());
  vi.stubGlobal("IDBKeyRange", IDBKeyRange);
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function server() {
  let committed: OwnershipReceipt | null = null;
  let loseResponse = false;
  let reject = false;
  const bodies: string[] = [];
  const transport = vi.fn<typeof fetch>(async (url, options) => {
    const path = new URL(String(url)).pathname;
    if (path.includes("/requests/"))
      return Response.json(committed ?? { error: "not_found" }, {
        status: committed ? 200 : 404,
      });
    if (!path.endsWith("/apply") || typeof options?.body !== "string")
      throw new Error(`Unexpected request: ${path}`);
    bodies.push(options.body);
    if (reject)
      return Response.json({ error: "preview_changed" }, { status: 409 });
    const input: OwnershipApply = JSON.parse(options.body);
    committed = receipt(input);
    if (loseResponse) {
      loseResponse = false;
      throw new TypeError("Response lost");
    }
    return Response.json({ review: committed, replayed: false });
  });
  return {
    transport,
    bodies,
    box: createAccountReviewOutbox(createAccountReviewAPI(endpoint, transport)),
    api: createSavedActionsAPI(endpoint, transport),
    loseResponse: () => {
      loseResponse = true;
    },
    reject: () => {
      reject = true;
    },
  };
}

it("discovers saved actions without sending requests or exposing their bodies", async () => {
  const remote = server();
  const saved = await remote.box.prepare(ids.account, preview());
  expect(await remote.api.page("account_ownership")).toEqual({
    items: [{ key: ids.account, state: "pending" }],
    next: null,
  });
  expect(await remote.api.page("account_merge")).toEqual({
    items: [],
    next: null,
  });
  expect(
    await createSavedActionsAPI(`${endpoint}another/`, remote.transport).page(
      "account_ownership",
    ),
  ).toEqual({ items: [], next: null });
  expect(remote.transport).not.toHaveBeenCalled();
  expect(await remote.box.read(ids.account)).toEqual(saved);
});

it("uses the original protocol to recover a lost reply without applying the action twice", async () => {
  const remote = server();
  const saved = await remote.box.prepare(ids.account, preview());
  remote.loseResponse();
  await expect(
    remote.api.recover("account_ownership", ids.account),
  ).rejects.toThrow("Response lost");
  const calls = remote.transport.mock.calls.length;
  expect((await remote.api.page("account_ownership")).items[0]?.state).toBe(
    "pending",
  );
  expect(remote.transport).toHaveBeenCalledTimes(calls);
  expect(await remote.api.recover("account_ownership", ids.account)).toBeNull();
  expect(remote.bodies).toEqual([saved.body]);
  expect(await remote.api.page("account_ownership")).toEqual({
    items: [],
    next: null,
  });
});

it("keeps a definitive rejection discoverable and does not replace or resend it", async () => {
  const remote = server();
  const saved = await remote.box.prepare(ids.account, preview());
  remote.reject();
  await expect(
    remote.api.recover("account_ownership", ids.account),
  ).rejects.toMatchObject({ code: "preview_changed" });
  expect((await remote.api.page("account_ownership")).items).toEqual([
    { key: ids.account, state: "rejected" },
  ]);
  const calls = remote.transport.mock.calls.length;
  await expect(
    remote.api.recover("account_ownership", ids.account),
  ).rejects.toMatchObject({ code: "preview_changed" });
  expect(remote.transport).toHaveBeenCalledTimes(calls);
  expect(remote.bodies).toEqual([saved.body]);
  expect((await remote.box.read(ids.account))?.body).toBe(saved.body);
});

it("preserves a damaged record while showing valid actions in the same page", async () => {
  const remote = server();
  await remote.box.prepare(ids.account, preview());
  const broken = { body: "sensitive malformed body", state: "pending" };
  await withReviewRecord(
    accountDatabase,
    ids.otherAccount,
    "readwrite",
    (_, store) => {
      store.add(broken, ids.otherAccount);
    },
  );
  const page = await remote.api.page("account_ownership");
  expect(page.items).toEqual(
    expect.arrayContaining([
      { key: ids.account, state: "pending" },
      { key: ids.otherAccount, state: "unreadable" },
    ]),
  );
  expect(JSON.stringify(page)).not.toContain("sensitive");
  expect(
    await withReviewRecord(
      accountDatabase,
      ids.otherAccount,
      "readonly",
      (value) => value,
    ),
  ).toEqual(broken);
  expect(remote.transport).not.toHaveBeenCalled();
});

it("bounds the page and advances past a request removed in another tab", async () => {
  const remote = server();
  const keys = Array.from(
    { length: 26 },
    (_, i) => `00000000-0000-4000-8000-${String(i + 1).padStart(12, "0")}`,
  );
  await withReviewRecord(accountDatabase, "", "readwrite", (_, store) => {
    for (const key of keys) store.add({ retained: true }, key);
  });
  const first = await remote.api.page("account_ownership");
  expect(first.items).toHaveLength(25);
  expect(first.next).toBe(keys[24]);
  if (!first.next) throw new Error("Expected another page");
  const cursor = first.next;
  await withReviewRecord(accountDatabase, cursor, "readwrite", (_, store) =>
    store.delete(cursor),
  );
  expect(await remote.api.page("account_ownership", first.next ?? "")).toEqual({
    items: [{ key: keys[25], state: "unreadable" }],
    next: null,
  });
  expect(remote.transport).not.toHaveBeenCalled();
});

it("honors cancellation before reading any local requests", async () => {
  const remote = server();
  const open = vi.spyOn(indexedDB, "open");
  await expect(
    remote.api.page("account_ownership", "", AbortSignal.abort()),
  ).rejects.toMatchObject({ name: "AbortError" });
  expect(open).not.toHaveBeenCalled();
  expect(remote.transport).not.toHaveBeenCalled();
});

it("preserves a new collection's label and opens its original creation request", async () => {
  const remote = server();
  const box = createCollectionOutbox(
    createCollectionAPI(endpoint, remote.transport),
  );
  const original = await box.prepare(collectionInput());
  expect(await remote.api.page("collection")).toEqual({
    items: [{ key: collectionID, state: "pending", label: "Purchased videos" }],
    next: null,
  });
  expect(await remote.api.location("collection", collectionID)).toEqual({
    kind: "collection",
    id: collectionID,
    create: true,
  });
  expect(await box.read(collectionID)).toEqual(original);
  expect(remote.transport).not.toHaveBeenCalled();
});

it("shows retained batches without claiming file jobs are completed", async () => {
  const remote = server();
  const box = createManualIntakeOutbox(
    createManualIntakeAPI(endpoint, remote.transport),
  );
  const preview = manualPreview("Purchased scene.mp4");
  await box.prepare([preview]);
  expect(await remote.api.page("manual_intake")).toEqual({
    items: [
      {
        key: preview.collection_uuid,
        state: "batch",
        files: 1,
        label: "Purchased scene.mp4",
      },
    ],
    next: null,
  });
  expect(
    await remote.api.location("manual_intake", preview.collection_uuid),
  ).toEqual({ kind: "collection", id: preview.collection_uuid, create: false });
  expect(remote.transport).not.toHaveBeenCalled();
});

it("routes local media scopes to the correct review without interpreting them as UUIDs", async () => {
  const remote = server();
  expect(await remote.api.location("metadata", "scene:123")).toEqual({
    kind: "scene",
    id: "123",
    tab: "metadata-review",
  });
  expect(await remote.api.location("source_link", "image:456")).toEqual({
    kind: "image",
    id: "456",
    tab: "source-review",
  });
  await expect(
    remote.api.location("metadata", "scene:1:extra"),
  ).rejects.toThrow();
  await expect(
    remote.api.location("account_ownership", "../elsewhere"),
  ).rejects.toThrow();
  expect(remote.transport).not.toHaveBeenCalled();
});

it("resolves attachment and notification review destinations with checked GETs", async () => {
  const attachment = mediaPreview().current;
  const merge = mergeReceipt();
  const transport = vi.fn<typeof fetch>(async (url, options) => {
    expect(options?.method).toBe("GET");
    const path = new URL(String(url)).pathname;
    if (path.endsWith(`/attachments/${attachment.attachment.uuid}/review`))
      return Response.json(attachment);
    if (
      path.endsWith(
        `/post-consolidation/requests/${merge.request.request_uuid}`,
      )
    )
      return Response.json(merge);
    throw new Error(`Unexpected lookup: ${path}`);
  });
  const api = createSavedActionsAPI(endpoint, transport);
  expect(
    await api.location("attachment_media", attachment.attachment.uuid),
  ).toEqual({ kind: "post", id: attachment.post_uuid });
  expect(
    await api.location("merge_notification", merge.request.request_uuid),
  ).toEqual({ kind: "post", id: merge.request.source_uuid });
  expect(transport).toHaveBeenCalledTimes(2);
});
