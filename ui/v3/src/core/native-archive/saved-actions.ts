import { z } from "zod";
import {
  createAccountReviewAPI,
  accountUUIDSchema as uuid,
} from "./account-review-api";
import { createAccountReviewOutbox } from "./account-review-outbox";
import { createAccountConsolidationAPI } from "./account-consolidation-api";
import { createAccountConsolidationOutbox } from "./account-consolidation-outbox";
import { createMetadataReviewAPI } from "./metadata-review-api";
import { createMetadataReviewOutbox } from "./metadata-review-outbox";
import { createSourceReviewAPI } from "./source-review-api";
import { createSourceReviewOutbox } from "./source-review-outbox";
import { createAlbumReviewAPI } from "./album-review-api";
import { createAlbumReviewOutbox } from "./album-review-outbox";
import { createAttachmentSelectionAPI } from "./attachment-selection-api";
import { createAttachmentSelectionOutbox } from "./attachment-selection-outbox";
import {
  createAttachmentMediaAPI,
  createGalleryAssociationAPI,
} from "./association-review-api";
import { createAssociationReviewOutbox } from "./association-review-outbox";
import { createPostConsolidationAPI } from "./post-consolidation-api";
import {
  createPostConsolidationOutbox,
  createMergeNotificationOutbox,
} from "./post-consolidation-outbox";
import { createCollectionAPI } from "./collection-api";
import { createCollectionOutbox } from "./collection-outbox";
import { createMediaRootAPI } from "./media-root-api";
import { createMediaRootOutbox } from "./media-root-outbox";
import { createMetadataPolicyAPI } from "./metadata-policy-api";
import { createMetadataPolicyOutbox } from "./metadata-policy-outbox";
import { createManualIntakeAPI } from "./manual-intake-api";
import { createManualIntakeOutbox } from "./manual-intake-outbox";
import { nativeArchiveEndpoint, NativeArchiveError } from "./client";
import { reviewKeys } from "./review-storage";

export const savedActionFamilySchema = z.enum([
  "account_ownership",
  "account_merge",
  "metadata",
  "source_link",
  "album",
  "attachment_selection",
  "gallery_association",
  "attachment_media",
  "post_merge",
  "merge_notification",
  "collection",
  "media_root",
  "metadata_policy",
  "manual_intake",
]);
export type SavedActionFamily = z.infer<typeof savedActionFamilySchema>;
export const savedActionSearchSchema = z.object({
  family: savedActionFamilySchema.default("account_ownership"),
});
export type SavedActionState =
  | "pending"
  | "rejected"
  | "admitted"
  | "batch"
  | "unreadable";
export interface SavedActionSummary {
  key: string;
  state: SavedActionState;
  files?: number;
  label?: string;
}
export type SavedActionLocation =
  | { kind: "account" | "post"; id: string }
  | { kind: "collection" | "root"; id: string; create: boolean }
  | {
      kind: "scene" | "image";
      id: string;
      tab: "metadata-review" | "source-review";
    };
export interface SavedActionPage {
  items: SavedActionSummary[];
  next: string | null;
}

interface Box<Saved> {
  read: (key: string) => Promise<Saved | null>;
  deliver: (key: string) => Promise<unknown>;
}
interface Adapter {
  database: string;
  read: (key: string) => Promise<SavedActionSummary | null>;
  deliver: (key: string) => Promise<unknown>;
}
type MediaTarget = { kind: "scene" | "image"; localId: string };

function mediaKey(key: string): MediaTarget {
  const [kind, localId] = z
    .tuple([z.enum(["scene", "image"]), z.string().regex(/^[1-9]\d*$/)])
    .parse(key.split(":"));
  return { kind, localId };
}
function mediaBox<Saved>(box: {
  read: (target: MediaTarget) => Promise<Saved | null>;
  deliver: (target: MediaTarget) => Promise<unknown>;
}): Box<Saved> {
  return {
    read: (key) => box.read(mediaKey(key)),
    deliver: (key) => box.deliver(mediaKey(key)),
  };
}

/** Inventory is local to this browser and installation. It never treats a saved
 * request as evidence of server failure or an unresolved archive association. */
export function createSavedActionsAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  function adapter<Saved>(
    prefix: string,
    box: Box<Saved>,
    project: (saved: Saved) => Omit<SavedActionSummary, "key">,
  ): Adapter {
    return {
      database: `${prefix}:${endpoint}`,
      async read(key) {
        const value = await box.read(key);
        return value === null ? null : { key, ...project(value) };
      },
      deliver: box.deliver,
    };
  }
  function simple<Saved extends { state: SavedActionState }>(
    prefix: string,
    box: Box<Saved>,
  ) {
    return adapter(prefix, box, (saved) => ({ state: saved.state }));
  }
  const merge = createPostConsolidationAPI(endpoint, transport);
  const collections = createCollectionOutbox(
    createCollectionAPI(endpoint, transport),
  );
  const roots = createMediaRootOutbox(createMediaRootAPI(endpoint, transport));
  const attachments = createAttachmentMediaAPI(endpoint, transport);
  function definition(value: { body: string }) {
    return z
      .object({
        label: z.string(),
        expected_revision: z.number().int().nonnegative(),
      })
      .parse(JSON.parse(value.body));
  }
  const adapters: Record<SavedActionFamily, Adapter> = {
    account_ownership: simple(
      "stash-account-review:v1",
      createAccountReviewOutbox(createAccountReviewAPI(endpoint, transport)),
    ),
    account_merge: simple(
      "stash-account-consolidation:v1",
      createAccountConsolidationOutbox(
        createAccountConsolidationAPI(endpoint, transport),
      ),
    ),
    metadata: simple(
      "stash-metadata-review:v1",
      mediaBox(
        createMetadataReviewOutbox(
          createMetadataReviewAPI(endpoint, transport),
        ),
      ),
    ),
    source_link: simple(
      "stash-source-review:v1",
      mediaBox(
        createSourceReviewOutbox(createSourceReviewAPI(endpoint, transport)),
      ),
    ),
    album: simple(
      "stash-album-review:v1",
      createAlbumReviewOutbox(createAlbumReviewAPI(endpoint, transport)),
    ),
    attachment_selection: simple(
      "stash-attachment-selection-review:v1",
      createAttachmentSelectionOutbox(
        createAttachmentSelectionAPI(endpoint, transport),
      ),
    ),
    gallery_association: simple(
      "stash-association-review:v1:gallery-association",
      createAssociationReviewOutbox(
        createGalleryAssociationAPI(endpoint, transport),
      ),
    ),
    attachment_media: simple(
      "stash-association-review:v1:attachment-media",
      createAssociationReviewOutbox(attachments),
    ),
    post_merge: simple(
      "stash-post-consolidation:v1",
      createPostConsolidationOutbox(merge),
    ),
    merge_notification: adapter(
      "stash-post-merge-notification:v1",
      createMergeNotificationOutbox(merge),
      () => ({ state: "pending" }),
    ),
    collection: adapter("stash-collections:v1", collections, (saved) => ({
      state: saved.state,
      label: definition(saved).label,
    })),
    media_root: adapter("stash-media-roots:v1", roots, (saved) => ({
      state: saved.state,
      label: definition(saved).label,
    })),
    metadata_policy: simple(
      "stash-metadata-policies:v1",
      createMetadataPolicyOutbox(createMetadataPolicyAPI(endpoint, transport)),
    ),
    manual_intake: adapter(
      "stash-manual-intake:v1",
      createManualIntakeOutbox(createManualIntakeAPI(endpoint, transport)),
      (saved) => ({
        state: "batch",
        files: saved.items.length,
        label: saved.items[0]?.preview.filename,
      }),
    ),
  };

  async function summary(selected: Adapter, key: string) {
    try {
      return await selected.read(key);
    } catch (error) {
      // A damaged record must remain discoverable without poisoning valid rows.
      // Storage failures still fail the page; they are not corrupt input.
      if (
        error instanceof z.ZodError ||
        error instanceof SyntaxError ||
        (error instanceof NativeArchiveError &&
          error.code === "invalid_saved_request")
      )
        return { key, state: "unreadable" as const };
      throw error;
    }
  }
  return {
    endpoint,
    async page(
      family: SavedActionFamily,
      after = "",
      signal?: AbortSignal,
    ): Promise<SavedActionPage> {
      const selected = adapters[savedActionFamilySchema.parse(family)];
      signal?.throwIfAborted();
      const page = await reviewKeys(selected.database, after, 25);
      signal?.throwIfAborted();
      const rows = await Promise.all(
        page.keys.map((key) => summary(selected, key)),
      );
      signal?.throwIfAborted();
      return { items: rows.filter((row) => row !== null), next: page.next };
    },
    async recover(family: SavedActionFamily, key: string) {
      const selected = adapters[savedActionFamilySchema.parse(family)];
      // The owning protocol reads the original bytes and checks its receipt.
      // No generic resend, discard, new request UUID or fresh preview is created.
      await selected.deliver(key);
      return selected.read(key);
    },
    async location(
      family: SavedActionFamily,
      key: string,
      signal?: AbortSignal,
    ): Promise<SavedActionLocation> {
      savedActionFamilySchema.parse(family);
      signal?.throwIfAborted();
      if (family === "metadata" || family === "source_link") {
        const target = mediaKey(key);
        return {
          kind: target.kind,
          id: target.localId,
          tab: family === "metadata" ? "metadata-review" : "source-review",
        };
      }
      uuid.parse(key);
      switch (family) {
        case "account_ownership":
        case "account_merge":
          return { kind: "account", id: key };
        case "collection": {
          const value = await collections.read(key);
          return {
            kind: "collection",
            id: key,
            create: !!value && definition(value).expected_revision === 0,
          };
        }
        case "media_root": {
          const value = await roots.read(key);
          return {
            kind: "root",
            id: key,
            create: !!value && definition(value).expected_revision === 0,
          };
        }
        case "metadata_policy":
        case "manual_intake":
          return { kind: "collection", id: key, create: false };
        case "attachment_media":
          return {
            kind: "post",
            id: (await attachments.context(key, signal)).post_uuid,
          };
        case "merge_notification":
          return {
            kind: "post",
            id: (await merge.review(key, signal)).request.source_uuid,
          };
        default:
          return { kind: "post", id: key };
      }
    },
  };
}
export type SavedActionsAPI = ReturnType<typeof createSavedActionsAPI>;
