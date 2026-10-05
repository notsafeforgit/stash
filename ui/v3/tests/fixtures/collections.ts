import {
  collectionSchema,
  type CollectionInput,
  type Collection,
  type MediaRoot,
} from "../../src/core/native-archive/collection-api";

export const collectionID = "60000000-0000-4000-8000-000000000001";
export const rootID = "70000000-0000-4000-8000-000000000001";
export function collectionInput(): CollectionInput {
  return {
    uuid: collectionID,
    expected_revision: 0,
    label: "Purchased videos",
    kind: "directory",
    state: "active",
    namespace: "",
    target_url: "",
    account_uuid: null,
    root_uuid: rootID,
    path_prefix: "Purchased/River",
    reason: "Initial folder",
  };
}
export function collection(input = collectionInput()): Collection {
  return collectionSchema.parse({
    ...input,
    revision: input.expected_revision + 1,
    created_at: "2026-10-05T12:00:00Z",
  });
}
export function mediaRoot(): MediaRoot {
  return {
    uuid: rootID,
    label: "Media library",
    state: "active",
    revision: 1,
    created_at: "2026-10-05T12:00:00Z",
    binding: { path: "/media/library", directory_identity: "directory:1" },
  };
}
