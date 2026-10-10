import { z } from "zod";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";
import { identitySchema } from "./metadata-review-api";

export const accountUUIDSchema = z
  .string()
  .regex(/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/)
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000");
const uuid = accountUUIDSchema;
const revision = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
const digest = z.string().regex(/^[0-9a-f]{64}$/);
export const ownershipStateSchema = z.enum(["linked", "unlinked", "undecided"]);
function accountText(maxBytes: number) {
  return z
    .string()
    .refine(
      (value) =>
        new TextEncoder().encode(value).length <= maxBytes &&
        !/\p{Cc}/u.test(value),
    );
}
export const ownershipReasonSchema = accountText(4096);
export const accountSearchSchema = z.object({
  q: accountText(256),
  namespace: z
    .string()
    .max(128)
    .regex(
      /^(?:(?:native|ytdl):[a-z0-9][a-z0-9_.-]*|(?:mirror|legacy):[a-z0-9][a-z0-9_.-]*:[a-z0-9][a-z0-9_.-]*|)$/,
    ),
  ownership: z.enum(["all", "linked", "unlinked", "undecided"]),
  scope: z.enum(["tracked", "all"]),
});
export const accountFilterSchema = accountSearchSchema.extend({
  q: accountSearchSchema.shape.q.default(""),
  namespace: accountSearchSchema.shape.namespace.default(""),
  ownership: accountSearchSchema.shape.ownership.default("undecided"),
  scope: accountSearchSchema.shape.scope.default("tracked"),
});
export const accountPerformerSchema = z.object({
  uuid,
  revision,
  state: z.enum(["active", "redirected", "deleted"]),
  local_id: revision.optional(),
  name: z.string(),
  disambiguation: z.string().optional(),
});
export const accountOwnershipSchema = z.object({
  decision_uuid: uuid,
  revision,
  state: ownershipStateSchema,
  performer_uuid: uuid.optional(),
  performer: accountPerformerSchema.optional(),
  origin: z.string(),
  reason: z.string(),
  created_at: z.string(),
});
export const accountIdentifierSchema = z.object({
  uuid,
  account_uuid: uuid,
  reference: z.object({
    namespace: z.string(),
    kind: z.string(),
    value: z.string(),
  }),
});
export const accountSchema = z.object({
  uuid,
  namespace: z.string(),
  label: z.string(),
  revision,
  canonical_uuid: uuid,
  redirect_to: uuid.optional(),
  ownership: accountOwnershipSchema.optional(),
  identifiers: z.array(accountIdentifierSchema).max(8),
  more_identifiers: z.boolean(),
  tracked: z.boolean(),
});
export const accountEvidenceSchema = z.object({
  key: z.string(),
  basis: z.string(),
  origin: z.string(),
  details: z.record(z.string(), z.unknown()),
  first_observed: z.string(),
  last_observed: z.string(),
});

const inputShape = z
  .object({
    account_uuid: uuid,
    account_revision: revision,
    state: ownershipStateSchema,
    performer_uuid: uuid.optional(),
    performer_revision: revision.optional(),
    reason: ownershipReasonSchema.optional(),
  })
  .strict();
function validTarget(input: z.infer<typeof inputShape>) {
  return input.state === "linked"
    ? input.performer_uuid !== undefined &&
        input.performer_revision !== undefined
    : input.performer_uuid === undefined &&
        input.performer_revision === undefined;
}
export const ownershipInputSchema = inputShape.refine(validTarget);
export const ownershipApplySchema = inputShape
  .extend({ request_uuid: uuid, digest })
  .strict()
  .refine(validTarget);
export const ownershipPreviewSchema = z.object({
  input: ownershipInputSchema,
  account: accountSchema,
  performer: accountPerformerSchema.optional(),
  digest,
});
export const ownershipReceiptSchema = z.object({
  request_uuid: uuid,
  decision_uuid: uuid,
  request: ownershipApplySchema,
  created_at: z.string(),
});
export type AccountFilter = z.infer<typeof accountFilterSchema>;
export type Account = z.infer<typeof accountSchema>;
export type AccountPerformer = z.infer<typeof accountPerformerSchema>;
export type AccountIdentifier = z.infer<typeof accountIdentifierSchema>;
export type AccountOwnership = z.infer<typeof accountOwnershipSchema>;
export type AccountEvidence = z.infer<typeof accountEvidenceSchema>;
export type OwnershipInput = z.infer<typeof ownershipInputSchema>;
export type OwnershipApply = z.infer<typeof ownershipApplySchema>;
export type OwnershipPreview = z.infer<typeof ownershipPreviewSchema>;
export type OwnershipReceipt = z.infer<typeof ownershipReceiptSchema>;

export function normalizeOwnershipInput(input: OwnershipInput): OwnershipInput {
  const ret = ownershipInputSchema.parse(input);
  if (ret.reason === "") delete ret.reason;
  return ret;
}
function inputKey(input: OwnershipInput) {
  return JSON.stringify(normalizeOwnershipInput(input));
}
export function ownershipRequestKey(input: OwnershipApply): string {
  const { request_uuid, digest, ...choice } = ownershipApplySchema.parse(input);
  return JSON.stringify([inputKey(choice), request_uuid, digest]);
}
function checkedReceipt(
  value: unknown,
  input: OwnershipApply,
): OwnershipReceipt {
  const receipt = ownershipReceiptSchema.parse(value);
  if (
    receipt.request_uuid !== input.request_uuid ||
    ownershipRequestKey(receipt.request) !== ownershipRequestKey(input)
  )
    throw new NativeArchiveError(0, "receipt_mismatch");
  return receipt;
}

export function createAccountReviewAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = globalThis.fetch.bind(globalThis),
) {
  const request = createArchiveRequest(endpoint, transport);
  const pageLimit = 25;
  return {
    endpoint,
    pageLimit,
    accounts(filter: AccountFilter, after = "", signal?: AbortSignal) {
      const valid = accountFilterSchema.parse(filter);
      const query = new URLSearchParams({
        limit: String(pageLimit),
        q: valid.q,
        namespace: valid.namespace,
        scope: valid.scope,
      });
      if (valid.ownership !== "all") query.set("ownership", valid.ownership);
      if (after) query.set("after", uuid.parse(after));
      return request(
        `source-accounts?${query}`,
        z.array(accountSchema).max(pageLimit),
        undefined,
        signal,
      );
    },
    async account(id: string, signal?: AbortSignal) {
      const valid = uuid.parse(id);
      const result = await request(
        `source-accounts/${valid}`,
        accountSchema,
        undefined,
        signal,
      );
      if (result.uuid !== valid)
        throw new NativeArchiveError(0, "account_mismatch");
      return result;
    },
    identifiers(id: string, after = "", signal?: AbortSignal) {
      const query = new URLSearchParams({ limit: String(pageLimit) });
      if (after) query.set("after", uuid.parse(after));
      return request(
        `source-accounts/${uuid.parse(id)}/identifiers?${query}`,
        z.array(accountIdentifierSchema).max(pageLimit),
        undefined,
        signal,
      );
    },
    evidence(id: string, after = "", signal?: AbortSignal) {
      const query = new URLSearchParams({
        limit: String(pageLimit),
        after: accountText(512).parse(after),
      });
      return request(
        `source-account-identifiers/${uuid.parse(id)}/evidence?${query}`,
        z.array(accountEvidenceSchema).max(pageLimit),
        undefined,
        signal,
      );
    },
    history(id: string, after = 0, signal?: AbortSignal) {
      z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER).parse(after);
      return request(
        `source-accounts/${uuid.parse(id)}/ownership-history?after=${after}&limit=${pageLimit}`,
        z.array(accountOwnershipSchema).max(pageLimit),
        undefined,
        signal,
      );
    },
    async performer(localID: string, signal?: AbortSignal) {
      if (!/^[1-9]\d*$/.test(localID))
        throw new NativeArchiveError(0, "invalid_identity");
      const identity = await request(
        `entity-identities/performer/${localID}`,
        identitySchema,
        undefined,
        signal,
      );
      if (
        identity.kind !== "performer" ||
        String(identity.local_id) !== localID
      )
        throw new NativeArchiveError(0, "identity_mismatch");
      return identity;
    },
    async preview(input: OwnershipInput, signal?: AbortSignal) {
      const valid = normalizeOwnershipInput(input);
      const result = await request(
        "account-ownership/preview",
        ownershipPreviewSchema,
        JSON.stringify(valid),
        signal,
      );
      if (
        inputKey(result.input) !== inputKey(valid) ||
        result.account.uuid !== valid.account_uuid ||
        result.account.canonical_uuid !== valid.account_uuid ||
        result.account.revision !== valid.account_revision ||
        (valid.state === "linked"
          ? result.performer === undefined ||
            result.performer.uuid !== valid.performer_uuid ||
            result.performer.revision !== valid.performer_revision ||
            result.performer.state !== "active"
          : result.performer !== undefined)
      )
        throw new NativeArchiveError(0, "preview_mismatch");
      return result;
    },
    async receipt(input: OwnershipApply, signal?: AbortSignal) {
      const valid = ownershipApplySchema.parse(input);
      try {
        return checkedReceipt(
          await request(
            `account-ownership/requests/${valid.request_uuid}`,
            ownershipReceiptSchema,
            undefined,
            signal,
          ),
          valid,
        );
      } catch (error) {
        if (error instanceof NativeArchiveError && error.status === 404)
          return null;
        throw error;
      }
    },
    async applySaved(body: string, signal?: AbortSignal) {
      if (new TextEncoder().encode(body).length > 16384)
        throw new NativeArchiveError(0, "request_too_large");
      const input = ownershipApplySchema.parse(JSON.parse(body));
      const result = await request(
        "account-ownership/apply",
        z.object({ review: ownershipReceiptSchema, replayed: z.boolean() }),
        body,
        signal,
      );
      return { ...result, review: checkedReceipt(result.review, input) };
    },
  };
}
export type AccountReviewAPI = ReturnType<typeof createAccountReviewAPI>;
