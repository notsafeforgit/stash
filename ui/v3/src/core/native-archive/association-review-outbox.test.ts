import { IDBFactory } from "fake-indexeddb";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import {
  createGalleryAssociationAPI,
  createAttachmentMediaAPI,
  galleryAssociationApplySchema,
  attachmentMediaApplySchema,
  type GalleryAssociationReceipt,
  type AttachmentMediaReceipt,
} from "./association-review-api";
import { createAssociationReviewOutbox } from "./association-review-outbox";
import {
  ids,
  galleryPreview,
  galleryReceipt,
  mediaPreview,
  mediaReceipt,
} from "../../../tests/fixtures/source-association";

const endpoint = "https://example.test/library/api/v3/archive/";
beforeEach(() => vi.stubGlobal("indexedDB", new IDBFactory()));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
type Family = "gallery" | "attachment";
function server(family: Family) {
  let committed: GalleryAssociationReceipt | AttachmentMediaReceipt | null =
    null;
  let loseResponse = false,
    loseBeforeCommit = false,
    unavailable = false,
    wrongReceipt = false;
  let refusal = "";
  const bodies: string[] = [];
  const transport = vi.fn<typeof fetch>(async (url, options) => {
    const path = new URL(String(url)).pathname;
    if (path.includes("/requests/"))
      return unavailable
        ? Response.json({ error: "unavailable" }, { status: 503 })
        : Response.json(committed ?? { error: "not_found" }, {
            status: committed ? 200 : 404,
          });
    if (!path.endsWith("/apply") || typeof options?.body !== "string")
      throw new Error(`Unexpected ${path}`);
    bodies.push(options.body);
    if (loseBeforeCommit) {
      loseBeforeCommit = false;
      throw new TypeError("Connection lost");
    }
    if (refusal) return Response.json({ error: refusal }, { status: 409 });
    committed =
      family === "gallery"
        ? galleryReceipt(
            galleryAssociationApplySchema.parse(JSON.parse(options.body)),
          )
        : mediaReceipt(
            attachmentMediaApplySchema.parse(JSON.parse(options.body)),
          );
    if (loseResponse) {
      loseResponse = false;
      throw new TypeError("Response lost");
    }
    return Response.json({
      review: wrongReceipt
        ? {
            ...committed,
            request: { ...committed.request, digest: "c".repeat(64) },
          }
        : committed,
      replayed: false,
    });
  });
  return {
    transport,
    bodies,
    loseResponse: () => {
      loseResponse = true;
    },
    loseBeforeCommit: () => {
      loseBeforeCommit = true;
    },
    unavailable: () => {
      unavailable = true;
    },
    wrongReceipt: () => {
      wrongReceipt = true;
    },
    refuse: (code: string) => {
      refusal = code;
    },
  };
}
function journal(
  family: Family,
  remote: ReturnType<typeof server>,
  base = endpoint,
) {
  if (family === "gallery") {
    const outbox = createAssociationReviewOutbox(
      createGalleryAssociationAPI(base, remote.transport),
    );
    return {
      read: () => outbox.read(ids.post),
      prepare: (reason?: string) =>
        outbox.prepare(ids.post, {
          ...galleryPreview(),
          input: { ...galleryPreview().input, reason },
        }),
      deliver: () => outbox.deliver(ids.post),
      forget: (request: string) => outbox.forgetRejected(ids.post, request),
    };
  }
  const outbox = createAssociationReviewOutbox(
    createAttachmentMediaAPI(base, remote.transport),
  );
  return {
    read: () => outbox.read(ids.attachment),
    prepare: (reason?: string) =>
      outbox.prepare(ids.attachment, {
        ...mediaPreview(),
        input: { ...mediaPreview().input, reason },
      }),
    deliver: () => outbox.deliver(ids.attachment),
    forget: (request: string) => outbox.forgetRejected(ids.attachment, request),
  };
}

it.each<Family>(["gallery", "attachment"])(
  "recovers %s saves after a lost response without a second apply",
  async (family) => {
    const remote = server(family);
    remote.loseResponse();
    const first = journal(family, remote),
      saved = await first.prepare();
    expect(remote.transport).not.toHaveBeenCalled();
    await expect(first.deliver()).rejects.toThrow("Response lost");
    const second = journal(family, remote);
    expect(await second.read()).toEqual(saved);
    expect((await second.deliver()).request_uuid).toBe(
      JSON.parse(saved.body).request_uuid,
    );
    expect(remote.bodies).toEqual([saved.body]);
    expect(await first.read()).toBeNull();
  },
);
it.each<Family>(["gallery", "attachment"])(
  "retries the same %s bytes after interruption before commit",
  async (family) => {
    const remote = server(family);
    remote.loseBeforeCommit();
    const first = journal(family, remote),
      saved = await first.prepare();
    await expect(first.deliver()).rejects.toThrow("Connection lost");
    await journal(family, remote).deliver();
    expect(remote.bodies).toEqual([saved.body, saved.body]);
  },
);
it.each<Family>(["gallery", "attachment"])(
  "serializes %s tabs and preserves the first pending intent",
  async (family) => {
    const remote = server(family),
      one = journal(family, remote),
      two = journal(family, remote);
    const [first, second] = await Promise.all([one.prepare(), two.prepare()]);
    expect(second).toEqual(first);
    await expect(two.prepare("Replace intent")).rejects.toMatchObject({
      code: "pending_review",
    });
    await expect(
      two.forget(JSON.parse(first.body).request_uuid),
    ).rejects.toMatchObject({ code: "pending_review" });
    expect(
      await journal(
        family,
        remote,
        "https://another.test/api/v3/archive/",
      ).read(),
    ).toBeNull();
  },
);
it.each<Family>(["gallery", "attachment"])(
  "keeps uncertain %s requests and permits only a proven stale preview to be cleared",
  async (family) => {
    const remote = server(family),
      box = journal(family, remote),
      saved = await box.prepare();
    remote.refuse("request_conflict");
    await expect(box.deliver()).rejects.toMatchObject({
      code: "request_conflict",
    });
    expect((await box.read())?.state).toBe("pending");
    remote.refuse("preview_changed");
    await expect(box.deliver()).rejects.toMatchObject({
      code: "preview_changed",
    });
    expect((await box.read())?.state).toBe("rejected");
    await expect(box.forget(ids.otherPost)).rejects.toMatchObject({
      code: "pending_review",
    });
    await box.forget(JSON.parse(saved.body).request_uuid);
    expect(await box.read()).toBeNull();
  },
);
it.each<Family>(["gallery", "attachment"])(
  "does not deliver %s while receipt status is unavailable",
  async (family) => {
    const remote = server(family),
      box = journal(family, remote);
    await box.prepare();
    remote.unavailable();
    await expect(box.deliver()).rejects.toMatchObject({ code: "unavailable" });
    expect(remote.bodies).toEqual([]);
    expect(await box.read()).not.toBeNull();
  },
);
it.each<Family>(["gallery", "attachment"])(
  "retains %s intent when the returned receipt differs",
  async (family) => {
    const remote = server(family),
      box = journal(family, remote),
      saved = await box.prepare();
    remote.wrongReceipt();
    await expect(box.deliver()).rejects.toMatchObject({
      code: "receipt_mismatch",
    });
    expect(await box.read()).toEqual(saved);
  },
);
