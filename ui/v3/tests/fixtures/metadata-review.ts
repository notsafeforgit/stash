import type {
  EditApply,
  EditPreview,
  EditReceipt,
} from "../../src/core/native-archive/metadata-review-api";

export const ids = {
  entity: "10000000-0000-4000-8000-000000000001",
  history: "10000000-0000-4000-8000-000000000002",
  match: "10000000-0000-4000-8000-000000000003",
  file: "10000000-0000-4000-8000-000000000004",
  collection: "10000000-0000-4000-8000-000000000005",
  decision: "10000000-0000-4000-8000-000000000006",
  performer: "20000000-0000-4000-8000-000000000007",
  thirdPerformer: "40000000-0000-4000-8000-000000000012",
  otherPerformer: "30000000-0000-4000-8000-000000000008",
};
export function preview(): EditPreview {
  return {
    input: {
      entity_uuid: ids.entity,
      history_uuid: ids.history,
      match_uuid: ids.match,
      source_field: "title",
    },
    entity_revision: 2,
    field: "title",
    current_value: "Library title",
    current_mode: "set",
    current_origin: "library",
    protected: true,
    mode: "set",
    value: "Retained title",
    file_uuid: ids.file,
    generation: 1,
    status: "ready",
    digest: "a".repeat(64),
  };
}
export function receipt(request: EditApply): EditReceipt {
  return {
    request_uuid: request.request_uuid,
    ...(request.keep_current
      ? { kept_current: true as const }
      : { decision_uuid: ids.decision }),
    field:
      request.source_field === "actors" ? "performers" : request.source_field,
    request,
    created_at: "2026-10-03T13:00:00Z",
  };
}
