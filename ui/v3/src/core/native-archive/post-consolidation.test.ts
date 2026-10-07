import { IDBFactory, IDBObjectStore } from "fake-indexeddb";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  mergeInput,
  mergePreview,
  mergeReceipt,
  mergeNotification,
} from "../../../tests/fixtures/post-consolidation";
import {
  createPostConsolidationAPI,
  type MergeNotification,
} from "./post-consolidation-api";
import {
  postMergeInputSchema,
  postMergePreviewSchema,
  postMergeReceiptSchema,
  type PostMergeApply,
  type PostMergeReceipt,
} from "./post-consolidation-schema";
import {
  createPostConsolidationOutbox,
  createMergeNotificationOutbox,
} from "./post-consolidation-outbox";

const endpoint = "https://example.test/library/api/v3/archive/";
const source = mergeInput().source_uuid;
beforeEach(() => vi.stubGlobal("indexedDB", new IDBFactory()));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("reads API-created history with omitted choice lists without treating it as the browser's exact request", async () => {
  const original = mergeReceipt();
  const external = {
    ...original,
    request: { ...original.request, media: null, attachments: null },
  };
  const api = createPostConsolidationAPI(
    endpoint,
    vi.fn(async () => Response.json(external)),
  );
  expect((await api.review(original.request.request_uuid)).request).toEqual(
    external.request,
  );
  await expect(api.receipt(JSON.stringify(original.request))).rejects.toThrow();
});

it("accepts real Go responses and rejects contradictory choices, original members and receipts", () => {
  expect(mergePreview().ready).toBe(true);
  expect(mergeReceipt().result.gallery.created).toBe(true);
  const input = mergeInput();
  expect(
    postMergeInputSchema.safeParse({ ...input, destination_uuid: source })
      .success,
  ).toBe(false);
  expect(
    postMergeInputSchema.safeParse({ ...input, selection: { mode: "choose" } })
      .success,
  ).toBe(false);
  expect(
    postMergeInputSchema.safeParse({
      ...input,
      attachments: [
        { namespace: "native:reddit", value: "first", state: "linked" },
      ],
    }).success,
  ).toBe(false);
  expect(
    postMergeInputSchema.safeParse({
      ...input,
      media: Array(2).fill({ media_uuid: source, state: "unlinked" }),
    }).success,
  ).toBe(false);
  const preview = mergePreview();
  expect(
    postMergePreviewSchema.safeParse({
      ...preview,
      blockers: [{ kind: "gallery_choice" }],
    }).success,
  ).toBe(false);
  const receipt = mergeReceipt();
  receipt.result.members[0]!.previous_revision++;
  expect(postMergeReceiptSchema.safeParse(receipt).success).toBe(false);
});

function server() {
  let committed: PostMergeReceipt | null = null;
  let loseResponse = false,
    loseBeforeCommit = false,
    unavailable = false;
  let refusal = "",
    wrongReceipt = false;
  const bodies: string[] = [];
  const transport = vi.fn<typeof fetch>(async (url, options) => {
    const path = new URL(String(url)).pathname;
    if (path.endsWith("/check"))
      return unavailable
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
    const input: PostMergeApply = JSON.parse(options.body);
    committed = mergeReceipt(input);
    if (loseResponse) {
      loseResponse = false;
      throw new TypeError("Response lost");
    }
    return Response.json({
      review: wrongReceipt
        ? mergeReceipt({ ...input, reason: "Another request" })
        : committed,
      replayed: false,
    });
  });
  return {
    api: createPostConsolidationAPI(endpoint, transport),
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
    unavailable: () => {
      unavailable = true;
    },
    wrongReceipt: () => {
      wrongReceipt = true;
    },
  };
}

it("persists before sending and recovers a lost committed response after reload without another merge", async () => {
  const remote = server();
  remote.loseResponse();
  const first = createPostConsolidationOutbox(remote.api);
  const saved = await first.prepare(source, mergeInput(), mergePreview());
  expect(remote.transport).not.toHaveBeenCalled();
  await expect(first.deliver(source)).rejects.toThrow("Response lost");
  const reloaded = createPostConsolidationOutbox(remote.api);
  expect(await reloaded.read(source)).toEqual(saved);
  expect((await reloaded.deliver(source)).request).toEqual(
    JSON.parse(saved.body),
  );
  expect(remote.bodies).toEqual([saved.body]);
  expect(await first.read(source)).toBeNull();
});

it("retries exactly the saved body when delivery failed before commit", async () => {
  const remote = server();
  remote.loseBeforeCommit();
  const box = createPostConsolidationOutbox(remote.api);
  const saved = await box.prepare(source, mergeInput(), mergePreview());
  await expect(box.deliver(source)).rejects.toThrow("Connection lost");
  await createPostConsolidationOutbox(remote.api).deliver(source);
  expect(remote.bodies).toEqual([saved.body, saved.body]);
});

it("coalesces matching tabs and protects pending choices from replacement or discard", async () => {
  const remote = server();
  const box = createPostConsolidationOutbox(remote.api);
  const [a, b] = await Promise.all([
    box.prepare(source, mergeInput(), mergePreview()),
    createPostConsolidationOutbox(remote.api).prepare(
      source,
      mergeInput(),
      mergePreview(),
    ),
  ]);
  expect(a).toEqual(b);
  await expect(
    box.prepare(
      source,
      { ...mergeInput(), reason: "Different choice" },
      mergePreview(),
    ),
  ).rejects.toMatchObject({ code: "pending_review" });
  await expect(
    box.forgetRejected(source, JSON.parse(a.body).request_uuid),
  ).rejects.toMatchObject({ code: "pending_review" });
  expect(await box.read(source)).toEqual(a);
});

it("scopes saved requests to the original post and server even after a redirect", async () => {
  const remote = server();
  const box = createPostConsolidationOutbox(remote.api);
  await box.prepare(source, mergeInput(), mergePreview());
  expect(await box.read(mergeInput().destination_uuid)).toBeNull();
  expect(
    await createPostConsolidationOutbox(
      createPostConsolidationAPI(`${endpoint}other/`, remote.transport),
    ).read(source),
  ).toBeNull();
});

it.each(["request_conflict", "unknown_refusal"])(
  "retains pending requests on %s",
  async (code) => {
    const remote = server();
    remote.refuse(code);
    const box = createPostConsolidationOutbox(remote.api);
    const saved = await box.prepare(source, mergeInput(), mergePreview());
    await expect(box.deliver(source)).rejects.toMatchObject({ code });
    expect(await box.read(source)).toEqual(saved);
  },
);

it("blocks delivery if its read-only check fails, and never accepts another request's receipt", async () => {
  const remote = server();
  const box = createPostConsolidationOutbox(remote.api);
  const saved = await box.prepare(source, mergeInput(), mergePreview());
  remote.wrongReceipt();
  await expect(box.deliver(source)).rejects.toMatchObject({
    code: "receipt_mismatch",
  });
  expect(await box.read(source)).toEqual(saved);
  remote.unavailable();
  await expect(box.deliver(source)).rejects.toMatchObject({ status: 503 });
  expect(remote.bodies).toHaveLength(1);
});

it("only discards a definitively refused stale preview and prevents writes when local persistence fails", async () => {
  const remote = server();
  remote.refuse("preview_changed");
  const box = createPostConsolidationOutbox(remote.api);
  const saved = await box.prepare(source, mergeInput(), mergePreview());
  await expect(box.deliver(source)).rejects.toMatchObject({
    code: "preview_changed",
  });
  expect((await box.read(source))?.state).toBe("rejected");
  await box.forgetRejected(source, JSON.parse(saved.body).request_uuid);
  expect(await box.read(source)).toBeNull();
  remote.bodies.length = 0;
  vi.spyOn(IDBObjectStore.prototype, "add").mockImplementation(() => {
    throw new DOMException("Full", "QuotaExceededError");
  });
  await expect(
    box.prepare(source, mergeInput(), mergePreview()),
  ).rejects.toThrow();
  expect(remote.bodies).toHaveLength(0);
});

it("recovers notification retries separately without reapplying the merge", async () => {
  const original = mergeNotification();
  let child: MergeNotification | null = null;
  const bodies: string[] = [];
  const transport = vi.fn<typeof fetch>(async (url, options) => {
    const path = new URL(String(url)).pathname;
    if (path.includes("/post-merge-notification-requests/"))
      return Response.json(child ?? { error: "not_found" }, {
        status: child ? 200 : 404,
      });
    if (
      !path.endsWith(`/${original.job_uuid}/retry`) ||
      typeof options?.body !== "string"
    )
      throw new Error(`Unexpected request: ${path}`);
    bodies.push(options.body);
    child = {
      ...original,
      job_uuid: source,
      sequence: 2,
      state: "queued",
      revision: 1,
      resume_from_job_uuid: original.job_uuid,
      resume_from_job_revision: original.revision,
    };
    throw new TypeError("Response lost");
  });
  const api = createPostConsolidationAPI(endpoint, transport),
    box = createMergeNotificationOutbox(api);
  const saved = await box.prepare(original);
  await expect(box.deliver(original.review_uuid)).rejects.toThrow(
    "Response lost",
  );
  const reloaded = createMergeNotificationOutbox(api);
  expect(await reloaded.read(original.review_uuid)).toEqual(saved);
  expect((await reloaded.deliver(original.review_uuid)).job_uuid).toBe(source);
  expect(bodies).toHaveLength(1);
  expect(await box.read(original.review_uuid)).toBeNull();
});

it("rejects notifications from another review and incorrectly ordered history", async () => {
  const original = mergeNotification();
  const api = createPostConsolidationAPI(
    endpoint,
    vi.fn(async () => Response.json([original, original])),
  );
  await expect(api.notifications(original.review_uuid)).rejects.toMatchObject({
    code: "invalid_response",
  });
  await expect(api.notifications(source)).rejects.toMatchObject({
    code: "invalid_response",
  });
  const wrong = createPostConsolidationAPI(
    endpoint,
    vi.fn(async () => Response.json({ ...original, job_uuid: source })),
  );
  await expect(
    wrong.notification(original.job_uuid, original.review_uuid),
  ).rejects.toMatchObject({ code: "receipt_mismatch" });
});
