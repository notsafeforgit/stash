import type {
  ManualDirectoryPage,
  ManualFilePreview,
  ManualFileRequest,
  ManualFileStatus,
} from "../../src/core/native-archive/manual-intake-api";
import { collectionID, rootID } from "./collections";
export const manualID = (n: number) =>
  `a0000000-0000-4000-8000-${String(n).padStart(12, "0")}`;
export const manualTime = "2026-10-06T20:00:00Z";
export function manualPreview(name = "Movie.mp4"): ManualFilePreview {
  return {
    collection_uuid: collectionID,
    relative_path: `Purchased/River/${name}`,
    media_kind: name.endsWith(".jpg") ? "image" : "scene",
    collection_revision: 1,
    policy_revision: 1,
    root_uuid: rootID,
    root_revision: 1,
    filename: name,
    size: 25600,
    modified_at: manualTime,
    file_signature: "c".repeat(64),
    signature: "a".repeat(64),
  };
}
export function manualStatus(input: ManualFileRequest): ManualFileStatus {
  return {
    ...input,
    job_uuid: manualID(3),
    state: "queued",
    revision: 1,
    attempts: 0,
    max_attempts: 8,
    available_at: manualTime,
    registration_committed: false,
    media_ingested: false,
    created_at: manualTime,
    updated_at: manualTime,
  };
}
export function completedManual(input: ManualFileStatus): ManualFileStatus {
  return {
    ...input,
    revision: input.revision + 3,
    state: "succeeded",
    registration_committed: true,
    media_ingested: true,
    publication: {
      file_uuid: manualID(4),
      generation: 1,
      content_uuid: manualID(5),
      media_uuid: manualID(6),
      media_kind: input.media_kind,
      media_created: true,
      file_linked: true,
      source_media: "none",
      gallery: "none",
      review: [],
    },
  };
}
export function manualDirectory(): ManualDirectoryPage {
  return {
    collection_uuid: collectionID,
    collection_revision: 1,
    root_uuid: rootID,
    root_revision: 1,
    path_prefix: "Purchased/River",
    directory: "Purchased/River",
    signature: "b".repeat(64),
    entries: [
      {
        name: "Albums",
        relative_path: "Purchased/River/Albums",
        kind: "directory",
        size: 0,
      },
      ...["Movie.mp4", "Photo.jpg"].map((name) => ({
        name,
        relative_path: `Purchased/River/${name}`,
        kind: name.endsWith(".jpg") ? ("image" as const) : ("scene" as const),
        size: 25600,
        modified_at: manualTime,
      })),
    ],
  };
}
