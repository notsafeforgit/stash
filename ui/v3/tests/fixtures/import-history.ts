import type {
  ImportDetails,
  ImportKind,
  ImportSnapshot,
} from "../../src/core/native-archive/import-history-api";
import { importFamilies } from "../../src/core/native-archive/import-history-api";

export const importIds = {
  snapshot: "10000000-0000-4000-8000-000000000001",
  later: "10000000-0000-4000-8000-000000000002",
  source: "20000000-0000-4000-8000-000000000001",
  registry: "30000000-0000-4000-8000-000000000001",
  collection: "40000000-0000-4000-8000-000000000001",
};

export function importSnapshot(kind: ImportKind = "catalog"): ImportSnapshot {
  return {
    kind,
    uuid: importIds.snapshot,
    source_uuid: importIds.source,
    registry_import_uuid: importIds.registry,
    collection_uuid: kind === "catalog" ? importIds.collection : null,
    collection_label: kind === "catalog" ? "Retained source collection" : null,
    captured_at: "2026-10-01T12:00:00Z",
    transfer_state: "received",
    chunks: 2,
    received_chunks: 2,
    records: 80,
    received_records: 80,
    bytes: 3200,
    received_bytes: 3200,
    created_at: "2026-10-02T12:00:00Z",
    updated_at: "2026-10-02T12:01:00Z",
  };
}

export function importDetails(kind: ImportKind = "catalog"): ImportDetails {
  return {
    snapshot: importSnapshot(kind),
    families: importFamilies[kind].map((name, index) => ({
      name,
      progress:
        index === 0
          ? {
              state: "review",
              source_records: 5,
              processed_records: 5,
              historical_review_records: 2,
              updated_at: "2026-10-02T12:05:00Z",
            }
          : null,
    })),
  };
}
