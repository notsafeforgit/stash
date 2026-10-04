import type { Account } from "../../src/core/native-archive/account-review-api";
import type {
  ConsolidationApply,
  ConsolidationPreview,
  ConsolidationReceipt,
} from "../../src/core/native-archive/account-consolidation-api";
import { account, ids, performer } from "./account-review";

export function destination(): Account {
  return {
    ...account(),
    uuid: ids.otherAccount,
    canonical_uuid: ids.otherAccount,
    label: "river-id",
    identifiers: [
      {
        uuid: ids.serviceID,
        account_uuid: ids.otherAccount,
        reference: {
          namespace: "native:reddit",
          kind: "id",
          value: "t2_river",
        },
      },
    ],
  };
}
export function consolidationPreview(other = false): ConsolidationPreview {
  const owner = performer(other);
  const ownership = {
    state: "linked" as const,
    performer_uuid: owner.uuid,
    performer_revision: owner.revision,
  };
  return {
    input: {
      source_uuid: ids.account,
      destination_uuid: ids.otherAccount,
      ownership_mode: "choose",
      ownership,
      accept_identifier_conflicts: false,
    },
    source: account(),
    destination: destination(),
    member_count: 2,
    identifier_count: 3,
    identifier_conflicts: [],
    ownership,
    performer: owner,
    blockers: [],
    ready: true,
    digest: "a".repeat(64),
  };
}
export function consolidationReceipt(
  request: ConsolidationApply,
): ConsolidationReceipt {
  return {
    request,
    consolidation: {
      uuid: request.request_uuid,
      sequence: 1,
      source_uuid: request.source_uuid,
      destination_uuid: request.destination_uuid,
      source_revision: 3,
      destination_revision: 3,
      ownership_decision_uuid: ids.decision,
      signature: request.digest,
      origin: "review",
      reason: request.reason ?? "",
      accepted_identifier_conflicts: request.accept_identifier_conflicts,
      created_at: "2026-10-03T20:00:00Z",
    },
  };
}
