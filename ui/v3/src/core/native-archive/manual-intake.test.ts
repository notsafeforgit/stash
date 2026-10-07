import { IDBFactory } from "fake-indexeddb";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  createManualIntakeAPI,
  manualFilePreviewSchema,
  type ManualFileStatus,
} from "./manual-intake-api";
import { createManualIntakeOutbox } from "./manual-intake-outbox";
import { collection, collectionID } from "../../../tests/fixtures/collections";
import {
  completedManual,
  manualDirectory,
  manualID,
  manualPreview,
  manualStatus,
} from "../../../tests/fixtures/manual-intake";
const endpoint = "https://example.test/library/api/v3/archive/";
beforeEach(() => vi.stubGlobal("indexedDB", new IDBFactory()));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("preserves automatic scan context through saved requests and rejects a changed receipt", async () => {
  const preview = {
    ...manualPreview(),
    scan_collection_uuid: manualID(80),
    scan_collection_revision: 2,
  };
  expect(
    manualFilePreviewSchema.safeParse({
      ...preview,
      scan_collection_revision: undefined,
    }).success,
  ).toBe(false);
  const remote = server();
  const outbox = createManualIntakeOutbox(remote.api);
  const saved = await outbox.prepare([preview]);
  const input = JSON.parse(saved.items[0]!.body);
  expect(input.scan_collection_uuid).toBe(preview.scan_collection_uuid);
  const status = await remote.api.submitSaved(saved.items[0]!.body);
  expect(status.scan_collection_uuid).toBe(preview.scan_collection_uuid);
  const mismatched = createManualIntakeAPI(endpoint, async () =>
    Response.json({ ...status, scan_collection_uuid: manualID(81) }),
  );
  await expect(
    mismatched.submitSaved(saved.items[0]!.body),
  ).rejects.toMatchObject({
    code: "mismatched_receipt",
  });
});
function server() {
  const statuses = new Map<string, ManualFileStatus>(),
    writes: string[] = [],
    calls: RequestInit[] = [];
  let lost = false,
    reject = "",
    cancelLost = false,
    badScope = false;
  const fetcher = vi.fn<typeof fetch>(async (url, options) => {
    calls.push(options ?? {});
    const path = new URL(String(url)).pathname.split("/archive/")[1]!;
    if (options?.method === "GET") {
      const status = statuses.get(path.split("/").at(-1)!);
      return status
        ? Response.json(
            badScope ? { ...status, relative_path: "elsewhere.mp4" } : status,
          )
        : Response.json({ error: "not_found" }, { status: 404 });
    }
    const body = String(options?.body);
    writes.push(body);
    const input = JSON.parse(body);
    if (path.endsWith("/retry")) {
      const prior = statuses.get(path.split("/").at(-2)!)!;
      if (
        prior.revision !== input.expected_revision ||
        !["failed", "cancelled"].includes(prior.state)
      )
        return Response.json(
          { error: "intake_request_changed" },
          { status: 409 },
        );
      const result: ManualFileStatus = {
        ...prior,
        request_uuid: input.request_uuid,
        job_uuid: manualID(90),
        state: "queued",
        revision: 1,
        attempts: 0,
        media_ingested: false,
        resume_from_job_uuid: prior.job_uuid,
        resume_from_revision: prior.revision,
      };
      statuses.set(result.request_uuid, result);
      if (lost) {
        lost = false;
        throw new TypeError("lost retry response");
      }
      return Response.json(result, { status: 202 });
    }
    if (path.endsWith("/cancel")) {
      const id = path.split("/").at(-2)!,
        prior = statuses.get(id)!;
      if (prior.revision !== input.expected_revision)
        return Response.json(
          { error: "intake_request_changed" },
          { status: 409 },
        );
      const next = {
        ...prior,
        state: "cancelled" as const,
        revision: prior.revision + 1,
      };
      statuses.set(id, next);
      if (cancelLost) {
        cancelLost = false;
        throw new TypeError("lost cancel response");
      }
      return Response.json(next);
    }
    if (reject) return Response.json({ error: reject }, { status: 409 });
    const result = manualStatus(input);
    statuses.set(input.request_uuid, result);
    if (lost) {
      lost = false;
      throw new TypeError("lost admission response");
    }
    return Response.json(result, { status: 202 });
  });
  return {
    api: createManualIntakeAPI(endpoint, fetcher),
    statuses,
    writes,
    calls,
    lose: () => {
      lost = true;
    },
    reject: (value: string) => {
      reject = value;
    },
    loseCancel: () => {
      cancelLost = true;
    },
    corrupt: () => {
      badScope = true;
    },
  };
}
it("persists a whole batch and recovers lost responses after reopening without duplicate admission", async () => {
  const s = server(),
    outbox = createManualIntakeOutbox(s.api);
  const saved = await outbox.prepare([
    manualPreview(),
    manualPreview("Photo.jpg"),
  ]);
  expect(s.writes).toHaveLength(0);
  s.lose();
  await expect(outbox.deliver(collectionID)).rejects.toThrow("lost admission");
  expect(s.writes).toHaveLength(2);
  const reloaded = createManualIntakeOutbox(s.api);
  await reloaded.inspect(collectionID);
  await reloaded.deliver(collectionID);
  expect(s.writes).toHaveLength(2);
  expect((await reloaded.read(collectionID))?.batch_uuid).toBe(
    saved.batch_uuid,
  );
  expect(
    (await reloaded.read(collectionID))?.items.every((item) => !!item.status),
  ).toBe(true);
  expect(
    s.calls.every(
      (call) =>
        call.credentials === "same-origin" &&
        call.cache === "no-store" &&
        call.redirect === "error",
    ),
  ).toBe(true);
  await expect(
    reloaded.dismiss(collectionID, saved.batch_uuid),
  ).rejects.toMatchObject({ code: "pending_review" });
});
it("only reads on inspection and serializes competing tabs", async () => {
  const s = server(),
    outbox = createManualIntakeOutbox(s.api);
  const [a, b] = await Promise.all([
    outbox.prepare([manualPreview()]),
    createManualIntakeOutbox(s.api).prepare([manualPreview()]),
  ]);
  expect(a).toEqual(b);
  await outbox.inspect(collectionID);
  expect(s.writes).toHaveLength(0);
  await expect(
    outbox.prepare([manualPreview("Photo.jpg")]),
  ).rejects.toMatchObject({ code: "pending_review" });
  await expect(
    outbox.prepare([
      manualPreview(),
      { ...manualPreview("Photo.jpg"), policy_revision: 2 },
    ]),
  ).rejects.toMatchObject({ code: "intake_preview_changed" });
});
it("retains uncertain conflicts and dismisses only resolved stale previews", async () => {
  const s = server(),
    outbox = createManualIntakeOutbox(s.api);
  const batch = await outbox.prepare([manualPreview()]);
  s.reject("intake_request_changed");
  await expect(outbox.deliver(collectionID)).rejects.toMatchObject({
    code: "intake_request_changed",
  });
  expect(
    (await outbox.read(collectionID))?.items[0]?.rejection,
  ).toBeUndefined();
  s.reject("intake_preview_changed");
  await expect(outbox.deliver(collectionID)).rejects.toMatchObject({
    code: "intake_preview_changed",
  });
  expect((await outbox.read(collectionID))?.items[0]?.rejection).toBe(
    "intake_preview_changed",
  );
  await outbox.dismiss(collectionID, batch.batch_uuid);
  expect(await outbox.read(collectionID)).toBeNull();
});
it("recovers a lost cancellation while retaining committed media", async () => {
  const s = server(),
    outbox = createManualIntakeOutbox(s.api);
  const batch = await outbox.prepare([manualPreview()]);
  await outbox.deliver(collectionID);
  const input = JSON.parse(batch.items[0]!.body);
  const result = {
    ...completedManual(s.statuses.get(input.request_uuid)!),
    state: "queued" as const,
    media_ingested: false,
  };
  s.statuses.set(result.request_uuid, result);
  await outbox.inspect(collectionID);
  s.loseCancel();
  await expect(outbox.cancel(collectionID, result)).rejects.toThrow(
    "lost cancel",
  );
  expect((await outbox.read(collectionID))?.items[0]?.cancel).toBeDefined();
  await createManualIntakeOutbox(s.api).inspect(collectionID);
  const recovered = (await outbox.read(collectionID))?.items[0];
  expect(recovered?.cancel).toBeUndefined();
  expect(recovered?.status?.state).toBe("cancelled");
  expect(recovered?.status?.publication).toEqual(result.publication);
  expect(s.writes).toHaveLength(2);
});
it("requires fresh cancellation after job progress and rejects mismatched receipts", async () => {
  const s = server(),
    outbox = createManualIntakeOutbox(s.api);
  const batch = await outbox.prepare([manualPreview()]);
  await outbox.deliver(collectionID);
  const input = JSON.parse(batch.items[0]!.body),
    prior = s.statuses.get(input.request_uuid)!;
  s.statuses.set(prior.request_uuid, {
    ...prior,
    revision: prior.revision + 1,
  });
  await expect(outbox.cancel(collectionID, prior)).rejects.toMatchObject({
    code: "intake_cancel_changed",
  });
  expect(s.writes).toHaveLength(1);
  expect((await outbox.read(collectionID))?.items[0]?.cancel).toBeUndefined();
  s.corrupt();
  await expect(outbox.inspect(collectionID)).rejects.toMatchObject({
    code: "mismatched_receipt",
  });
  expect(
    (await outbox.read(collectionID))?.items[0]?.status?.relative_path,
  ).toBe(prior.relative_path);
});
it("validates folder scope, ordering and immediate children", async () => {
  let result = manualDirectory();
  const api = createManualIntakeAPI(
    endpoint,
    vi.fn<typeof fetch>(async () => Response.json(result)),
  );
  expect(
    (await api.directory(collection(), "Purchased/River")).entries,
  ).toHaveLength(3);
  result = {
    ...manualDirectory(),
    entries: [...manualDirectory().entries].reverse(),
  };
  await expect(
    api.directory(collection(), "Purchased/River"),
  ).rejects.toMatchObject({ code: "directory_mismatch" });
  result = { ...manualDirectory(), collection_uuid: manualID(20) };
  await expect(
    api.directory(collection(), "Purchased/River"),
  ).rejects.toMatchObject({ code: "directory_mismatch" });
  result = {
    ...manualDirectory(),
    entries: [
      {
        ...manualDirectory().entries[1]!,
        relative_path: "Purchased/Other/Movie.mp4",
      },
    ],
  };
  await expect(
    api.directory(collection(), "Purchased/River"),
  ).rejects.toMatchObject({ code: "directory_mismatch" });
});

it("accepts non-BMP filenames in the server's UTF-8 order", async () => {
  const result = manualDirectory();
  result.entries = ["\ue000.jpg", "😀.jpg"].map((name) => ({
    name,
    relative_path: `Purchased/River/${name}`,
    kind: "image",
    size: 10,
    modified_at: "2026-10-06T20:00:00Z",
  }));
  const api = createManualIntakeAPI(
    endpoint,
    vi.fn<typeof fetch>(async () => Response.json(result)),
  );
  expect(
    (await api.directory(collection(), "Purchased/River")).entries.map(
      (entry) => entry.name,
    ),
  ).toEqual(["\ue000.jpg", "😀.jpg"]);
});

it("recovers a lost retry with the same request and preserved publication", async () => {
  const s = server(),
    outbox = createManualIntakeOutbox(s.api);
  const batch = await outbox.prepare([manualPreview()]);
  await outbox.deliver(collectionID);
  const original = s.statuses.get(
    JSON.parse(batch.items[0]!.body).request_uuid,
  )!;
  const failed: ManualFileStatus = {
    ...completedManual(original),
    state: "failed",
    media_ingested: false,
  };
  s.statuses.set(failed.request_uuid, failed);
  await outbox.inspect(collectionID);
  s.lose();
  await expect(outbox.retry(collectionID, failed)).rejects.toThrow(
    "lost retry",
  );
  const saved = await outbox.read(collectionID);
  expect(saved?.items[0]?.retry?.prior).toEqual(failed);
  expect(saved?.items[0]?.status).toBeUndefined();
  await createManualIntakeOutbox(s.api).deliver(collectionID);
  expect(s.writes).toHaveLength(2);
  const recovered = (await outbox.read(collectionID))!.items[0]!.status!;
  expect(recovered.request_uuid).not.toBe(failed.request_uuid);
  expect(recovered.resume_from_job_uuid).toBe(failed.job_uuid);
  expect(recovered.publication).toEqual(failed.publication);
  expect(s.statuses.get(failed.request_uuid)).toEqual(failed);
  await expect(outbox.retry(collectionID, failed)).rejects.toMatchObject({
    code: "invalid_saved_request",
  });
});
