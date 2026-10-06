import { z } from "zod";
import { createArchiveRequest, NativeArchiveError } from "./client";

export interface AssociationInput {
  post_uuid: string;
  post_revision: number;
  reason?: string;
}
export interface AssociationRequest {
  request_uuid: string;
  digest: string;
}
export interface AssociationPreview<Input> {
  input: Input;
  changed: boolean;
  digest: string;
}
export interface AssociationReceipt<Apply> {
  request_uuid: string;
  decision_uuid: string;
  request: Apply;
  created_at: string;
}

// The two association editors share transport/recovery rules, while their
// schemas retain distinct targets, revision guards and preview invariants.
export function createAssociationProtocol<
  Input extends AssociationInput,
  Apply extends Input & AssociationRequest,
  Preview extends AssociationPreview<Input>,
  Receipt extends AssociationReceipt<Apply>,
>(
  family: "gallery-association" | "attachment-media",
  endpoint: string,
  transport: typeof fetch,
  schemas: {
    input: z.ZodType<Input>;
    apply: z.ZodType<Apply>;
    preview: z.ZodType<Preview>;
    receipt: z.ZodType<Receipt>;
  },
  scope: (input: Input) => string,
) {
  const request = createArchiveRequest(endpoint, transport);
  function normalizeInput(input: unknown): Input {
    const result = schemas.input.parse(input);
    if (result.reason === "") delete result.reason;
    return result;
  }
  function inputKey(input: unknown) {
    return JSON.stringify(normalizeInput(input));
  }
  function requestKey(input: unknown) {
    const { request_uuid, digest, ...choice } = schemas.apply.parse(input);
    return JSON.stringify([inputKey(choice), request_uuid, digest]);
  }
  function checkedReceipt(value: unknown, input: Apply): Receipt {
    const receipt = schemas.receipt.parse(value);
    if (
      receipt.request_uuid !== input.request_uuid ||
      requestKey(receipt.request) !== requestKey(input)
    )
      throw new NativeArchiveError(0, "receipt_mismatch");
    return receipt;
  }
  return {
    family,
    endpoint,
    scope,
    normalizeInput,
    requestKey,
    parseApply: (input: unknown) => schemas.apply.parse(input),
    parsePreview: (input: unknown) => schemas.preview.parse(input),
    async preview(input: Input, signal?: AbortSignal) {
      const checked = normalizeInput(input);
      const result = await request(
        `${family}/preview`,
        schemas.preview,
        JSON.stringify(checked),
        signal,
      );
      if (inputKey(checked) !== inputKey(result.input))
        throw new NativeArchiveError(0, "preview_mismatch");
      return result;
    },
    async receipt(input: Apply, signal?: AbortSignal) {
      const checked = schemas.apply.parse(input);
      try {
        return checkedReceipt(
          await request(
            `${family}/requests/${checked.request_uuid}`,
            schemas.receipt,
            undefined,
            signal,
          ),
          checked,
        );
      } catch (error) {
        if (
          error instanceof NativeArchiveError &&
          error.status === 404 &&
          error.code === "not_found"
        )
          return null;
        throw error;
      }
    },
    async applySaved(body: string, signal?: AbortSignal) {
      if (new TextEncoder().encode(body).length > 16384)
        throw new NativeArchiveError(0, "request_too_large");
      const input = schemas.apply.parse(JSON.parse(body));
      const result = await request(
        `${family}/apply`,
        z.object({ review: schemas.receipt, replayed: z.boolean() }),
        body,
        signal,
      );
      return { ...result, review: checkedReceipt(result.review, input) };
    },
  };
}

export type AssociationProtocol<
  Input extends AssociationInput,
  Apply extends Input & AssociationRequest,
  Preview extends AssociationPreview<Input>,
  Receipt extends AssociationReceipt<Apply>,
> = ReturnType<
  typeof createAssociationProtocol<Input, Apply, Preview, Receipt>
>;
