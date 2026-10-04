import type {
  Account,
  AccountPerformer,
  OwnershipApply,
  OwnershipPreview,
  OwnershipReceipt,
} from "../../src/core/native-archive/account-review-api";

export const ids = {
  account: "10000000-0000-4000-8000-000000000001",
  otherAccount: "10000000-0000-4000-8000-000000000002",
  handle: "10000000-0000-4000-8000-000000000011",
  serviceID: "10000000-0000-4000-8000-000000000012",
  decision: "10000000-0000-4000-8000-000000000021",
  performer: "20000000-0000-4000-8000-000000000010",
  otherPerformer: "20000000-0000-4000-8000-000000000011",
  request: "30000000-0000-4000-8000-000000000001",
};

export function performer(other = false): AccountPerformer {
  return {
    uuid: other ? ids.otherPerformer : ids.performer,
    revision: 3,
    state: "active",
    local_id: other ? 11 : 10,
    name: "River",
    disambiguation: other ? "Photographer" : "Model",
  };
}

export function account(): Account {
  return {
    uuid: ids.account,
    canonical_uuid: ids.account,
    revision: 2,
    namespace: "native:reddit",
    label: "river",
    identifiers: [
      {
        uuid: ids.handle,
        account_uuid: ids.account,
        reference: {
          namespace: "native:reddit",
          kind: "handle",
          value: "river",
        },
      },
      {
        uuid: ids.serviceID,
        account_uuid: ids.account,
        reference: {
          namespace: "native:reddit",
          kind: "id",
          value: "t2_river",
        },
      },
    ],
    more_identifiers: false,
  };
}

export function preview(other = false): OwnershipPreview {
  const owner = performer(other);
  return {
    input: {
      account_uuid: ids.account,
      account_revision: 2,
      state: "linked",
      performer_uuid: owner.uuid,
      performer_revision: owner.revision,
    },
    account: account(),
    performer: owner,
    digest: "a".repeat(64),
  };
}

export function receipt(request: OwnershipApply): OwnershipReceipt {
  return {
    request_uuid: request.request_uuid,
    decision_uuid: ids.decision,
    request,
    created_at: "2026-10-03T13:00:00Z",
  };
}

export function linkedAccount(request: OwnershipApply): Account {
  return {
    ...account(),
    revision: request.account_revision + 1,
    ownership: {
      decision_uuid: ids.decision,
      revision: 1,
      state: request.state,
      ...(request.state === "linked"
        ? {
            performer_uuid: request.performer_uuid,
            performer: performer(request.performer_uuid === ids.otherPerformer),
          }
        : {}),
      origin: "review",
      reason: request.reason ?? "",
      created_at: "2026-10-03T13:00:00Z",
    },
  };
}
