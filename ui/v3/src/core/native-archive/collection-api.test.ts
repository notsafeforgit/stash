import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { IDBFactory, IDBObjectStore } from "fake-indexeddb";
import {
  createCollectionAPI,
  collectionInputSchema,
  type Collection,
  type CollectionInput,
  type CollectionRevision,
} from "./collection-api";
import { createCollectionOutbox } from "./collection-outbox";
import {
  collection,
  collectionInput,
  collectionID,
  mediaRoot,
  rootID,
} from "../../../tests/fixtures/collections";

const endpoint = "https://example.test/stash/api/v3/archive/";
beforeEach(() => vi.stubGlobal("indexedDB", new IDBFactory()));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function server() {
  let current: Collection | null = null;
  const history: CollectionRevision[] = [];
  let loseBefore = false;
  let loseAfter = false;
  const bodies: string[] = [];
  function commit(input: CollectionInput) {
    current = collection(input);
    history.push({
      ...current,
      origin: "review",
      reason: input.reason,
      recorded_at: "2026-10-05T13:00:00Z",
    });
  }
  const transport = vi.fn<typeof fetch>(async (target, options) => {
    const url = new URL(String(target));
    if (options?.method === "PUT") {
      const body = String(options.body);
      bodies.push(body);
      if (loseBefore) {
        loseBefore = false;
        throw new TypeError("Connection lost before commit");
      }
      const input = collectionInputSchema.parse(JSON.parse(body));
      if (input.expected_revision !== (current?.revision ?? 0))
        return Response.json({ error: "preview_changed" }, { status: 409 });
      commit(input);
      if (loseAfter) {
        loseAfter = false;
        throw new TypeError("Response lost after commit");
      }
      return Response.json(current);
    }
    if (!current) return Response.json({ error: "not_found" }, { status: 404 });
    return Response.json(
      url.pathname.endsWith("/history")
        ? history
            .filter(
              (row) => row.revision > Number(url.searchParams.get("after")),
            )
            .slice(0, Number(url.searchParams.get("limit")))
        : current,
    );
  });
  return {
    api: createCollectionAPI(endpoint, transport),
    transport,
    bodies,
    commit,
    loseBefore: () => {
      loseBefore = true;
    },
    loseAfter: () => {
      loseAfter = true;
    },
  };
}

it("uses bounded scoped searches, cancellation and PUT with the caller's stable identity", async () => {
  const transport = vi.fn<typeof fetch>(async (target, options) => {
    const url = new URL(String(target));
    if (options?.method === "PUT") return Response.json(collection());
    if (url.pathname.endsWith(rootID)) return Response.json(mediaRoot());
    return Response.json([]);
  });
  const api = createCollectionAPI(endpoint, transport);
  const controller = new AbortController();
  await api.collections(
    { q: "River %_", kind: "directory", state: "active" },
    collectionID,
    controller.signal,
  );
  await api.roots("Library", controller.signal);
  await api.root(rootID);
  await api.save(JSON.stringify(collectionInput()));
  const urls = transport.mock.calls.map(([target]) => new URL(String(target)));
  expect(Object.fromEntries(urls[0]?.searchParams ?? [])).toEqual({
    q: "River %_",
    kind: "directory",
    state: "active",
    after: collectionID,
    limit: "25",
  });
  expect(urls[1]?.pathname).toBe("/stash/api/v3/archive/media-roots");
  expect(transport.mock.calls[0]?.[1]).toMatchObject({
    method: "GET",
    signal: controller.signal,
    credentials: "same-origin",
    cache: "no-store",
    redirect: "error",
  });
  expect(transport.mock.calls[3]?.[1]?.method).toBe("PUT");
  expect(urls[3]?.pathname).toBe(
    `/stash/api/v3/archive/collections/${collectionID}`,
  );
});

it("rejects unsafe paths, services, settings leakage and unqualified account choices", () => {
  const input = collectionInput();
  for (const invalid of [
    { ...input, path_prefix: "/outside" },
    { ...input, path_prefix: "../outside" },
    { ...input, path_prefix: "a//b" },
    { ...input, path_prefix: "a\\b" },
    { ...input, root_uuid: null },
    { ...input, account_uuid: rootID },
    { ...input, namespace: "reddit" },
    { ...input, target_url: "https://user:pass@example.test/" },
    { ...input, target_url: "javascript:alert(1)" },
    { ...input, label: "猫".repeat(342) },
    { ...input, reason: "bad\nreason" },
    { ...input, settings: {} },
  ])
    expect(collectionInputSchema.safeParse(invalid).success).toBe(false);
  expect(
    collectionInputSchema.safeParse({
      ...input,
      root_uuid: null,
      path_prefix: "",
    }).success,
  ).toBe(true);
});

it("rejects mismatched identities, revisions and oversized pages", async () => {
  const wrong = createCollectionAPI(endpoint, async () =>
    Response.json({ ...collection(), uuid: rootID }),
  );
  await expect(wrong.collection(collectionID)).rejects.toMatchObject({
    code: "collection_mismatch",
  });
  await expect(
    wrong.save(JSON.stringify(collectionInput())),
  ).rejects.toMatchObject({ code: "collection_mismatch" });
  const many = createCollectionAPI(endpoint, async () =>
    Response.json(Array.from({ length: 26 }, () => collection())),
  );
  await expect(
    many.collections({ q: "", state: "", kind: "" }),
  ).rejects.toMatchObject({ code: "invalid_response" });
  const unrelated = createCollectionAPI(endpoint, async () =>
    Response.json([
      {
        ...collection(),
        uuid: rootID,
        origin: "review",
        reason: "",
        recorded_at: "now",
      },
    ]),
  );
  await expect(unrelated.history(collectionID)).rejects.toMatchObject({
    code: "history_mismatch",
  });
});

it("recovers the exact committed revision after reload and a subsequent edit without another write", async () => {
  const remote = server();
  remote.loseAfter();
  const first = createCollectionOutbox(remote.api);
  await first.prepare(collectionInput());
  expect(remote.transport).not.toHaveBeenCalled();
  await expect(first.deliver(collectionID)).rejects.toThrow("Response lost");
  remote.commit({
    ...collectionInput(),
    expected_revision: 1,
    label: "Edited in another tab",
    reason: "Later edit",
  });
  const second = createCollectionOutbox(remote.api);
  expect(await second.deliver(collectionID)).toMatchObject({
    label: "Purchased videos",
    revision: 1,
  });
  expect(remote.bodies).toHaveLength(1);
  expect(await second.read(collectionID)).toBeNull();
  expect(await remote.api.collection(collectionID)).toMatchObject({
    label: "Edited in another tab",
    revision: 2,
  });
});

it("retries an uncommitted creation with exactly the same UUID and bytes, serializing competing tabs", async () => {
  const remote = server();
  remote.loseBefore();
  const first = createCollectionOutbox(remote.api);
  const saved = await first.prepare(collectionInput());
  await expect(first.deliver(collectionID)).rejects.toThrow("before commit");
  const second = createCollectionOutbox(remote.api);
  await expect(
    second.prepare({ ...collectionInput(), label: "A different folder" }),
  ).rejects.toMatchObject({ code: "pending_review" });
  await second.deliver(collectionID);
  expect(remote.bodies).toEqual([saved.body, saved.body]);
});

it("rejects stale definitions instead of overwriting them and permits explicit review after rejection", async () => {
  const remote = server();
  remote.commit({ ...collectionInput(), label: "Other writer" });
  const outbox = createCollectionOutbox(remote.api);
  const saved = await outbox.prepare(collectionInput());
  await expect(outbox.deliver(collectionID)).rejects.toMatchObject({
    code: "preview_changed",
  });
  expect((await outbox.read(collectionID))?.state).toBe("rejected");
  expect(remote.bodies).toEqual([]);
  await outbox.forgetRejected(collectionID, saved.body);
  expect(await outbox.read(collectionID)).toBeNull();
});

it("does not send an update when the current definition already has the requested values", async () => {
  const remote = server();
  remote.commit(collectionInput());
  const outbox = createCollectionOutbox(remote.api);
  await outbox.prepare({
    ...collectionInput(),
    expected_revision: 1,
    reason: "No metadata change",
  });
  expect((await outbox.deliver(collectionID)).revision).toBe(1);
  expect(remote.bodies).toEqual([]);
});

it("keeps uncertain requests pending and requires durable storage before transmission", async () => {
  const remote = server();
  const outbox = createCollectionOutbox(remote.api);
  const add = vi
    .spyOn(IDBObjectStore.prototype, "add")
    .mockImplementation(() => {
      throw new Error("Storage full");
    });
  await expect(outbox.prepare(collectionInput())).rejects.toThrow(
    "Storage full",
  );
  expect(remote.transport).not.toHaveBeenCalled();
  add.mockRestore();
  const saved = await outbox.prepare(collectionInput());
  remote.transport.mockImplementationOnce(async () =>
    Response.json({ error: "unavailable" }, { status: 503 }),
  );
  await expect(outbox.deliver(collectionID)).rejects.toMatchObject({
    status: 503,
  });
  expect((await outbox.read(collectionID))?.state).toBe("pending");
  await expect(
    outbox.forgetRejected(collectionID, saved.body),
  ).rejects.toMatchObject({ code: "pending_review" });
  expect(remote.bodies).toEqual([]);
});
