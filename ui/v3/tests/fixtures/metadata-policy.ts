import type {
  MetadataPolicy,
  PolicyDefinition,
  PolicyField,
} from "../../src/core/native-archive/metadata-policy-api";
import { collectionID } from "./collections";

export const policyIDs = {
  entity: "80000000-0000-4000-8000-000000000001",
  image: "80000000-0000-4000-8000-000000000002",
  performer: "80000000-0000-4000-8000-000000000003",
  file: "80000000-0000-4000-8000-000000000004",
  capture: "80000000-0000-4000-8000-000000000005",
  attachment: "80000000-0000-4000-8000-000000000006",
  post: "80000000-0000-4000-8000-000000000007",
  postMediaDecision: "80000000-0000-4000-8000-000000000008",
};
export const policyFields: PolicyField[] = [
  { name: "title", type: "string", clear_value: "" },
  {
    name: "performers",
    type: "references",
    reference_kind: "performer",
    clear_value: [],
  },
  { name: "organized", type: "boolean", clear_value: false },
  {
    name: "studio",
    type: "reference",
    clear_value: null,
    reference_kind: "studio",
  },
  { name: "tags", type: "references", clear_value: [], reference_kind: "tag" },
  { name: "groups", type: "groups", clear_value: [], reference_kind: "group" },
];
export function policyDefinition(): PolicyDefinition {
  return {
    enabled: true,
    apply_to_scans: true,
    rules: {
      scene: {
        on_create: true,
        on_existing: true,
        skip_organized_on_create: false,
        mark_organized: false,
        filename_title_fallback: true,
        mappings: { title: { jq: ".source.metadata.title // empty" } },
      },
    },
  };
}
export function metadataPolicy(): MetadataPolicy {
  return {
    collection_uuid: collectionID,
    collection_revision: 1,
    revision: 1,
    definition: policyDefinition(),
    origin: "review",
    reason: "Initial rules",
    created_at: "2026-10-05T20:00:00Z",
  };
}
