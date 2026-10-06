import { expect, it, vi } from "vitest";
import {
  createGalleryAssociationAPI,
  createAttachmentMediaAPI,
  attachmentMediaContextSchema,
} from "./association-review-api";
import {
  ids,
  galleryPreview,
  galleryReceipt,
  mediaPreview,
  mediaReceipt,
} from "../../../tests/fixtures/source-association";

const endpoint = "https://example.test/library/api/v3/archive/";
const galleryInput = () => ({
  ...galleryPreview().input,
  request_uuid: ids.request,
  digest: galleryPreview().digest,
});
const mediaInput = () => ({
  ...mediaPreview().input,
  request_uuid: ids.request,
  digest: mediaPreview().digest,
});

it("uses session transport and the deployment prefix for gallery previews", async () => {
  const transport = vi.fn<typeof fetch>(async () =>
    Response.json(galleryPreview()),
  );
  expect(
    await createGalleryAssociationAPI(endpoint, transport).preview(
      galleryPreview().input,
    ),
  ).toEqual(galleryPreview());
  expect(String(transport.mock.calls[0]?.[0])).toBe(
    `${endpoint}gallery-association/preview`,
  );
  expect(transport.mock.calls[0]?.[1]).toMatchObject({
    method: "POST",
    credentials: "same-origin",
    body: JSON.stringify(galleryPreview().input),
  });
});

it("accepts converted media while rejecting another attachment or target", async () => {
  const api = createAttachmentMediaAPI(endpoint, async () =>
    Response.json(mediaPreview()),
  );
  expect((await api.preview(mediaPreview().input)).proposed?.kind).toBe(
    "scene",
  );
  await expect(
    api.preview({ ...mediaPreview().input, attachment_uuid: ids.otherPost }),
  ).rejects.toMatchObject({ code: "preview_mismatch" });
  await expect(
    createAttachmentMediaAPI(endpoint, async () =>
      Response.json({ ...mediaPreview(), proposed: galleryPreview().proposed }),
    ).preview(mediaPreview().input),
  ).rejects.toThrow();
});

it("allows a retained choice older than new attachment evidence", () => {
  const context = mediaPreview().current;
  const current = {
    uuid: ids.decision,
    attachment_uuid: ids.attachment,
    revision: 1,
    state: "linked",
    media_uuid: ids.media,
    origin: "review",
    reason: "",
    created_at: "2026-10-06T15:00:00Z",
  };
  expect(
    attachmentMediaContextSchema.parse({
      ...context,
      current,
      media: mediaPreview().proposed,
      post_link_state: "undecided",
    }).current?.revision,
  ).toBe(1);
  expect(() =>
    attachmentMediaContextSchema.parse({
      ...context,
      current: { ...current, revision: 3 },
      media: mediaPreview().proposed,
      post_link_state: "undecided",
    }),
  ).toThrow();
});

it("does not treat omitted-target choices as arbitrary field edits", async () => {
  const transport = vi.fn<typeof fetch>();
  await expect(
    createGalleryAssociationAPI(endpoint, transport).preview({
      ...galleryPreview().input,
      state: "disabled",
    }),
  ).rejects.toThrow();
  await expect(
    createAttachmentMediaAPI(endpoint, transport).preview({
      ...mediaPreview().input,
      state: "unlinked",
    }),
  ).rejects.toThrow();
  expect(transport).not.toHaveBeenCalled();
});

it("checks the original saved body and receipt for both association families", async () => {
  const galleryBody = JSON.stringify(galleryInput(), null, 2);
  const mediaBody = JSON.stringify(mediaInput(), null, 2);
  const galleryTransport = vi.fn<typeof fetch>(async () =>
    Response.json({ review: galleryReceipt(galleryInput()), replayed: false }),
  );
  const mediaTransport = vi.fn<typeof fetch>(async () =>
    Response.json({ review: mediaReceipt(mediaInput()), replayed: false }),
  );
  expect(
    (
      await createGalleryAssociationAPI(endpoint, galleryTransport).applySaved(
        galleryBody,
      )
    ).review.request_uuid,
  ).toBe(ids.request);
  expect(
    (
      await createAttachmentMediaAPI(endpoint, mediaTransport).applySaved(
        mediaBody,
      )
    ).review.request_uuid,
  ).toBe(ids.request);
  expect(galleryTransport.mock.calls[0]?.[1]?.body).toBe(galleryBody);
  expect(mediaTransport.mock.calls[0]?.[1]?.body).toBe(mediaBody);
  await expect(
    createGalleryAssociationAPI(endpoint, async () =>
      Response.json(galleryReceipt({ ...galleryInput(), reason: "Different" })),
    ).receipt(galleryInput()),
  ).rejects.toMatchObject({ code: "receipt_mismatch" });
  await expect(
    createAttachmentMediaAPI(endpoint, async () =>
      Response.json(
        mediaReceipt({ ...mediaInput(), media_uuid: ids.otherPost }),
      ),
    ).receipt(mediaInput()),
  ).rejects.toMatchObject({ code: "receipt_mismatch" });
});

it("requires a specific missing-receipt response before permitting retry", async () => {
  expect(
    await createGalleryAssociationAPI(endpoint, async () =>
      Response.json({ error: "not_found" }, { status: 404 }),
    ).receipt(galleryInput()),
  ).toBeNull();
  expect(
    await createAttachmentMediaAPI(endpoint, async () =>
      Response.json({ error: "not_found" }, { status: 404 }),
    ).receipt(mediaInput()),
  ).toBeNull();
  await expect(
    createAttachmentMediaAPI(endpoint, async () =>
      Response.json({ error: "route_missing" }, { status: 404 }),
    ).receipt(mediaInput()),
  ).rejects.toMatchObject({ code: "route_missing" });
});

it("rejects wrong contexts and non-increasing or foreign history", async () => {
  await expect(
    createAttachmentMediaAPI(endpoint, async () =>
      Response.json(mediaPreview().current),
    ).context(ids.otherPost),
  ).rejects.toMatchObject({ code: "invalid_response" });
  const history = {
    uuid: ids.decision,
    attachment_uuid: ids.attachment,
    revision: 3,
    state: "linked",
    media_uuid: ids.media,
    origin: "review",
    reason: "",
    created_at: "2026-10-06T15:00:00Z",
  };
  const api = createAttachmentMediaAPI(endpoint, async () =>
    Response.json([history]),
  );
  expect(await api.history(ids.attachment)).toEqual([history]);
  await expect(api.history(ids.attachment, 3)).rejects.toMatchObject({
    code: "invalid_response",
  });
  await expect(api.history(ids.otherPost)).rejects.toMatchObject({
    code: "invalid_response",
  });
  const galleryHistory = {
    post_uuid: ids.post,
    decision_uuid: ids.decision,
    revision: 4,
    state: "linked",
    gallery_uuid: ids.gallery,
    gallery: null,
    selection_uuid: null,
    origin: "review",
    reason: "",
    created_at: history.created_at,
  };
  expect(
    await createGalleryAssociationAPI(endpoint, async () =>
      Response.json([galleryHistory]),
    ).history(ids.post),
  ).toEqual([galleryHistory]);
  await expect(
    createGalleryAssociationAPI(endpoint, async () =>
      Response.json([galleryHistory, galleryHistory]),
    ).history(ids.post),
  ).rejects.toMatchObject({ code: "invalid_response" });
});
