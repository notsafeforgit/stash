import { beforeEach, afterEach, it, expect, vi } from "vitest";
import { IDBFactory, IDBObjectStore } from "fake-indexeddb";
import {
  createMediaRootAPI,
  mediaRootInputSchema,
  type MediaRootInput,
  type MediaRootRevision,
} from "./media-root-api";
import { createMediaRootOutbox } from "./media-root-outbox";
import { mediaRoot, rootID } from "../../../tests/fixtures/collections";

const endpoint = "https://example.test/stash/api/v3/archive/";
const input = (): MediaRootInput => ({
  uuid: rootID,
  expected_revision: 0,
  label: "Media library",
  state: "active",
  binding: { path: "/media/library", directory_identity: "directory:1" },
  reason: "Checked folder",
});
beforeEach(() => vi.stubGlobal("indexedDB", new IDBFactory()));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function server() {
  const rows: MediaRootRevision[] = [];
  const bodies: string[] = [];
  let lost: "before" | "after" | null = null;
  let mounted = true;
  let probed = false;
  function commit(value: MediaRootInput) {
    const found: MediaRootRevision = {
      ...mediaRoot(),
      uuid: value.uuid,
      label: value.label,
      state: value.state,
      binding: value.binding,
      revision: value.expected_revision + 1,
      origin: "review",
      reason: value.reason,
      recorded_at: "2026-10-05T21:00:00Z",
    };
    rows.push(found);
    return found;
  }
  const transport = vi.fn<typeof fetch>(async (target, options) => {
    const url = new URL(String(target));
    if (url.pathname.endsWith("/probe")) {
      probed = true;
      return Response.json(input().binding);
    }
    if (options?.method === "PUT") {
      bodies.push(String(options.body));
      if (lost === "before") {
        lost = null;
        throw new TypeError("lost before save");
      }
      const value = mediaRootInputSchema.parse(
        JSON.parse(String(options.body)),
      );
      if (value.expected_revision !== (rows.at(-1)?.revision ?? 0))
        return Response.json({ error: "preview_changed" }, { status: 409 });
      if (!mounted)
        return Response.json(
          { error: "invalid_root_binding", message: "The directory changed" },
          { status: 400 },
        );
      const result = commit(value);
      if (lost === "after") {
        lost = null;
        throw new TypeError("lost after save");
      }
      return Response.json(result);
    }
    if (!rows.length)
      return Response.json({ error: "not_found" }, { status: 404 });
    if (url.pathname.endsWith("/history"))
      return Response.json(
        rows
          .filter((row) => row.revision > Number(url.searchParams.get("after")))
          .slice(0, Number(url.searchParams.get("limit"))),
      );
    return Response.json(rows.at(-1));
  });
  return {
    api: createMediaRootAPI(endpoint, transport),
    transport,
    bodies,
    commit,
    lose: (when: "before" | "after") => {
      lost = when;
    },
    unmount: () => {
      mounted = false;
    },
    probed: () => probed,
  };
}

it("recovers the committed binding after a lost response, later relocation and mount loss without reprobe", async () => {
  const remote = server();
  const outbox = createMediaRootOutbox(remote.api);
  remote.lose("after");
  await outbox.prepare(input());
  await expect(outbox.deliver(rootID)).rejects.toThrow("lost after save");
  remote.commit({
    ...input(),
    expected_revision: 1,
    binding: { path: "/new/location", directory_identity: "directory:1" },
    reason: "Relocated",
  });
  remote.unmount();
  expect(await createMediaRootOutbox(remote.api).deliver(rootID)).toMatchObject(
    { revision: 1, binding: input().binding },
  );
  expect(remote.bodies).toHaveLength(1);
  expect(remote.probed()).toBe(false);
  expect(await outbox.read(rootID)).toBeNull();
});

it("retries exact checked binding bytes and rejects changed directories instead of silently probing again", async () => {
  const remote = server();
  const outbox = createMediaRootOutbox(remote.api);
  remote.lose("before");
  const saved = await outbox.prepare(input());
  await expect(outbox.deliver(rootID)).rejects.toThrow("lost before save");
  remote.unmount();
  await expect(outbox.deliver(rootID)).rejects.toMatchObject({
    status: 400,
    code: "invalid_root_binding",
    detail: "The directory changed",
  });
  expect(remote.bodies).toEqual([saved.body, saved.body]);
  expect(remote.probed()).toBe(false);
  expect((await outbox.read(rootID))?.state).toBe("rejected");
  await outbox.forgetRejected(rootID, saved.body);
  expect(await outbox.read(rootID)).toBeNull();
});

it("requires durable storage and isolates browser tabs and backend mounts", async () => {
  const remote = server();
  const one = createMediaRootOutbox(remote.api);
  const add = vi
    .spyOn(IDBObjectStore.prototype, "add")
    .mockImplementation(() => {
      throw new Error("storage unavailable");
    });
  await expect(one.prepare(input())).rejects.toThrow("storage unavailable");
  expect(remote.transport).not.toHaveBeenCalled();
  add.mockRestore();
  const saved = await one.prepare(input());
  const two = createMediaRootOutbox(remote.api);
  expect(await two.prepare(input())).toEqual(saved);
  await expect(
    two.prepare({ ...input(), label: "Another root" }),
  ).rejects.toMatchObject({ code: "pending_review" });
  const elsewhere = createMediaRootOutbox(
    createMediaRootAPI(
      "https://example.test/other/api/v3/archive/",
      remote.transport,
    ),
  );
  expect(await elsewhere.read(rootID)).toBeNull();
});

it("holds competing historical definitions for explicit review even when the current label matches", async () => {
  const remote = server();
  remote.commit({
    ...input(),
    binding: { path: "/elsewhere", directory_identity: "directory:2" },
  });
  const outbox = createMediaRootOutbox(remote.api);
  await outbox.prepare(input());
  await expect(outbox.deliver(rootID)).rejects.toMatchObject({
    code: "preview_changed",
  });
  expect(remote.bodies).toEqual([]);
  expect((await outbox.read(rootID))?.state).toBe("rejected");
});

it("validates bounded lookup pages and saved identities", async () => {
  const transport = vi.fn<typeof fetch>(async () =>
    Response.json([mediaRoot(), mediaRoot()]),
  );
  const api = createMediaRootAPI(endpoint, transport);
  await expect(api.roots({ q: "%_", state: "active" })).rejects.toMatchObject({
    code: "root_mismatch",
  });
  const url = new URL(String(transport.mock.calls[0]?.[0]));
  expect(url.searchParams.get("q")).toBe("%_");
  expect(url.searchParams.get("limit")).toBe("25");
  transport.mockImplementation(async () =>
    Response.json({
      ...mediaRoot(),
      uuid: "90000000-0000-4000-8000-000000000001",
    }),
  );
  await expect(api.root(rootID)).rejects.toMatchObject({
    code: "root_mismatch",
  });
  await expect(api.save(JSON.stringify(input()))).rejects.toMatchObject({
    code: "root_mismatch",
  });
  await expect(api.history(rootID, 0, 101)).rejects.toThrow();
});
