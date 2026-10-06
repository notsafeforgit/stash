import { z } from "zod";
import {
  accountSchema,
  accountPerformerSchema,
  accountUUIDSchema as uuid,
  createAccountReviewAPI,
} from "./account-review-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";

const integer = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
const timestamp = z.string().datetime({ offset: true });
const performer = accountPerformerSchema.refine(
  (value) =>
    value.state !== "redirected" &&
    (value.state === "active") === (value.local_id !== undefined),
);
const scope = z.object({ requested_uuid: uuid, performer });
export const performerAccountsSchema = scope
  .extend({ accounts: z.array(accountSchema).max(25) })
  .refine((page) =>
    page.accounts.every((account) => {
      const owner = account.ownership?.performer;
      return (
        account.uuid === account.canonical_uuid &&
        !account.redirect_to &&
        account.ownership?.state === "linked" &&
        owner?.uuid === page.performer.uuid &&
        owner.revision === page.performer.revision &&
        owner.state === page.performer.state &&
        owner.local_id === page.performer.local_id
      );
    }),
  );
export const performerIdentitySchema = z
  .object({
    uuid,
    revision: integer,
    state: z.enum(["active", "redirected", "deleted"]),
    original_id: integer.nullable(),
    redirect_to: uuid.nullable(),
    created_at: timestamp,
    retired_at: timestamp.nullable(),
  })
  .refine(
    (row) =>
      (row.state === "redirected") === (row.redirect_to !== null) &&
      (row.state === "active") === (row.retired_at === null) &&
      row.uuid !== row.redirect_to,
  );
export const performerIdentitiesSchema = scope.extend({
  identities: z.array(performerIdentitySchema).max(25),
});
export type PerformerAccounts = z.infer<typeof performerAccountsSchema>;
export type PerformerIdentities = z.infer<typeof performerIdentitiesSchema>;
export type PerformerIdentity = z.infer<typeof performerIdentitySchema>;

function checkPage<T extends { uuid: string }>(
  requested: string,
  actual: string,
  rows: T[],
  after = "",
) {
  if (requested !== actual)
    throw new NativeArchiveError(0, "identity_mismatch");
  let previous = after;
  for (const row of rows) {
    if (row.uuid <= previous)
      throw new NativeArchiveError(0, "invalid_response");
    previous = row.uuid;
  }
}

export function createPerformerSourceAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  const review = createAccountReviewAPI(endpoint, transport);
  const pageLimit = 25;
  function page(after?: string) {
    const params = new URLSearchParams({ limit: String(pageLimit) });
    if (after) params.set("after", uuid.parse(after));
    return params;
  }
  return {
    pageLimit,
    identity: review.performer,
    async accounts(id: string, after?: string, signal?: AbortSignal) {
      const result = await request(
        `entities/${uuid.parse(id)}/source-accounts?${page(after)}`,
        performerAccountsSchema,
        undefined,
        signal,
      );
      checkPage(id, result.requested_uuid, result.accounts, after);
      return result;
    },
    async identities(id: string, after?: string, signal?: AbortSignal) {
      const result = await request(
        `entities/${uuid.parse(id)}/performer-identities?${page(after)}`,
        performerIdentitiesSchema,
        undefined,
        signal,
      );
      checkPage(id, result.requested_uuid, result.identities, after);
      if (
        result.identities.some((row) =>
          row.uuid === result.performer.uuid
            ? row.state !== result.performer.state ||
              row.revision !== result.performer.revision
            : row.state !== "redirected",
        )
      )
        throw new NativeArchiveError(0, "invalid_response");
      return result;
    },
  };
}
export type PerformerSourceAPI = ReturnType<typeof createPerformerSourceAPI>;
