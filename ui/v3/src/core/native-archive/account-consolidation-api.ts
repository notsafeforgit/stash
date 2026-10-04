import { z } from "zod";
import {
  accountSchema,
  accountPerformerSchema,
  accountUUIDSchema as uuid,
  ownershipReasonSchema,
  ownershipStateSchema,
} from "./account-review-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";

const revision = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
const digest = z.string().regex(/^[0-9a-f]{64}$/);
const choiceSchema = z
  .object({
    state: ownershipStateSchema,
    performer_uuid: uuid.optional(),
    performer_revision: revision.optional(),
  })
  .strict()
  .refine((value) =>
    value.state === "linked"
      ? value.performer_uuid !== undefined &&
        value.performer_revision !== undefined
      : value.performer_uuid === undefined &&
        value.performer_revision === undefined,
  );
const inputShape = z
  .object({
    source_uuid: uuid,
    destination_uuid: uuid,
    ownership_mode: z.enum(["preserve", "choose"]),
    ownership: choiceSchema.optional(),
    accept_identifier_conflicts: z.boolean(),
    reason: ownershipReasonSchema.optional(),
  })
  .strict();
function validChoice(value: z.infer<typeof inputShape>) {
  return (
    value.source_uuid !== value.destination_uuid &&
    (value.ownership_mode === "choose"
      ? value.ownership !== undefined
      : value.ownership === undefined)
  );
}
export const consolidationInputSchema = inputShape.refine(validChoice);
export const consolidationApplySchema = inputShape
  .extend({ request_uuid: uuid, digest })
  .strict()
  .refine(validChoice);
export const consolidationPreviewSchema = z
  .object({
    input: consolidationInputSchema,
    source: accountSchema,
    destination: accountSchema,
    member_count: z.number().int().min(2).max(4096),
    identifier_count: z.number().int().min(0).max(8192),
    identifier_conflicts: z
      .array(
        z.object({
          namespace: z.string(),
          kind: z.string(),
          values: z.array(z.string()).min(2).max(8192),
        }),
      )
      .max(8192),
    ownership: choiceSchema.optional(),
    performer: accountPerformerSchema.optional(),
    blockers: z.array(z.enum(["identifiers", "ownership"])).max(2),
    ready: z.boolean(),
    digest,
  })
  .refine((value) => {
    const { input, ownership, performer, source, destination } = value;
    const blockers = [
      ...(value.identifier_conflicts.length &&
      !input.accept_identifier_conflicts
        ? ["identifiers"]
        : []),
      ...(!ownership ? ["ownership"] : []),
    ];
    return (
      source.uuid === input.source_uuid &&
      destination.uuid === input.destination_uuid &&
      source.canonical_uuid === source.uuid &&
      !source.redirect_to &&
      destination.canonical_uuid === destination.uuid &&
      !destination.redirect_to &&
      source.namespace === destination.namespace &&
      JSON.stringify(blockers) === JSON.stringify(value.blockers) &&
      value.ready === (blockers.length === 0) &&
      (input.ownership_mode !== "choose" ||
        JSON.stringify(input.ownership) === JSON.stringify(ownership)) &&
      (ownership?.state === "linked"
        ? performer?.uuid === ownership.performer_uuid &&
          performer?.revision === ownership.performer_revision &&
          performer?.state === "active"
        : performer === undefined)
    );
  });
export const consolidationRecordSchema = z.object({
  uuid,
  sequence: revision,
  source_uuid: uuid,
  destination_uuid: uuid,
  source_revision: revision,
  destination_revision: revision,
  ownership_decision_uuid: uuid,
  signature: digest,
  origin: z.enum(["review", "migration"]),
  reason: z.string(),
  accepted_identifier_conflicts: z.boolean(),
  created_at: z.string(),
});
export const consolidationReceiptSchema = z.object({
  request: consolidationApplySchema,
  consolidation: consolidationRecordSchema,
});
export type ConsolidationInput = z.infer<typeof consolidationInputSchema>;
export type ConsolidationApply = z.infer<typeof consolidationApplySchema>;
export type ConsolidationPreview = z.infer<typeof consolidationPreviewSchema>;
export type ConsolidationReceipt = z.infer<typeof consolidationReceiptSchema>;
export type ConsolidationRecord = z.infer<typeof consolidationRecordSchema>;

export function normalizeConsolidationInput(
  input: ConsolidationInput,
): ConsolidationInput {
  const ret = consolidationInputSchema.parse(input);
  if (ret.reason === "") delete ret.reason;
  return ret;
}
function inputKey(input: ConsolidationInput) {
  return JSON.stringify(normalizeConsolidationInput(input));
}
export function consolidationRequestKey(input: ConsolidationApply) {
  const { request_uuid, digest, ...choice } =
    consolidationApplySchema.parse(input);
  return JSON.stringify([inputKey(choice), request_uuid, digest]);
}
function checkedReceipt(
  value: unknown,
  input: ConsolidationApply,
): ConsolidationReceipt {
  const result = consolidationReceiptSchema.parse(value);
  const event = result.consolidation;
  if (
    consolidationRequestKey(result.request) !==
      consolidationRequestKey(input) ||
    event.uuid !== input.request_uuid ||
    event.source_uuid !== input.source_uuid ||
    event.destination_uuid !== input.destination_uuid ||
    event.signature !== input.digest ||
    event.origin !== "review" ||
    event.reason !== (input.reason ?? "") ||
    event.accepted_identifier_conflicts !== input.accept_identifier_conflicts
  )
    throw new NativeArchiveError(0, "receipt_mismatch");
  return result;
}
function savedInput(body: string) {
  if (new TextEncoder().encode(body).length > 16384)
    throw new NativeArchiveError(0, "request_too_large");
  return consolidationApplySchema.parse(JSON.parse(body));
}

export function createAccountConsolidationAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = globalThis.fetch.bind(globalThis),
) {
  const request = createArchiveRequest(endpoint, transport);
  const pageLimit = 25;
  return {
    endpoint,
    pageLimit,
    async preview(input: ConsolidationInput, signal?: AbortSignal) {
      const valid = normalizeConsolidationInput(input);
      const result = await request(
        "account-consolidation/preview",
        consolidationPreviewSchema,
        JSON.stringify(valid),
        signal,
      );
      if (inputKey(result.input) !== inputKey(valid))
        throw new NativeArchiveError(0, "preview_mismatch");
      return result;
    },
    async receipt(body: string, signal?: AbortSignal) {
      const input = savedInput(body);
      try {
        return checkedReceipt(
          await request(
            `account-consolidation/requests/${input.request_uuid}/check`,
            consolidationReceiptSchema,
            body,
            signal,
          ),
          input,
        );
      } catch (error) {
        if (error instanceof NativeArchiveError && error.status === 404)
          return null;
        throw error;
      }
    },
    async applySaved(body: string, signal?: AbortSignal) {
      const input = savedInput(body);
      const result = await request(
        "account-consolidation/apply",
        z.object({ review: consolidationReceiptSchema, replayed: z.boolean() }),
        body,
        signal,
      );
      return { ...result, review: checkedReceipt(result.review, input) };
    },
    history(id: string, after = 0, signal?: AbortSignal) {
      z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER).parse(after);
      return request(
        `source-accounts/${uuid.parse(id)}/consolidation-history?after=${after}&limit=${pageLimit}`,
        z.array(consolidationRecordSchema).max(pageLimit),
        undefined,
        signal,
      );
    },
  };
}
export type AccountConsolidationAPI = ReturnType<
  typeof createAccountConsolidationAPI
>;
