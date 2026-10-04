import { expect, it, vi } from "vitest";
import {
  createAccountConsolidationAPI,
  consolidationPreviewSchema,
  type ConsolidationApply,
} from "./account-consolidation-api";
import { ids } from "../../../tests/fixtures/account-review";
import {
  consolidationPreview as preview,
  consolidationReceipt as receipt,
} from "../../../tests/fixtures/account-consolidation";

const endpoint = "https://example.test/library/api/v3/archive/";
function input(): ConsolidationApply {
  return {
    ...preview().input,
    request_uuid: ids.request,
    digest: preview().digest,
  };
}

it("checks the exact saved body through a read-only POST before applying", async () => {
  const saved = JSON.stringify(input(), null, 2);
  const transport = vi.fn<typeof fetch>(async () =>
    Response.json(receipt(input())),
  );
  const api = createAccountConsolidationAPI(endpoint, transport);
  expect(await api.receipt(saved)).toEqual(receipt(input()));
  expect(transport).toHaveBeenCalledWith(
    new URL(`${endpoint}account-consolidation/requests/${ids.request}/check`),
    expect.objectContaining({
      body: saved,
      method: "POST",
      cache: "no-store",
      credentials: "same-origin",
      redirect: "error",
    }),
  );
});

it("distinguishes missing receipts from failed checks", async () => {
  const transport = vi
    .fn<typeof fetch>()
    .mockResolvedValueOnce(
      Response.json({ error: "not_found" }, { status: 404 }),
    )
    .mockResolvedValueOnce(
      Response.json({ error: "request_conflict" }, { status: 409 }),
    );
  const api = createAccountConsolidationAPI(endpoint, transport);
  expect(await api.receipt(JSON.stringify(input()))).toBeNull();
  await expect(api.receipt(JSON.stringify(input()))).rejects.toMatchObject({
    status: 409,
    code: "request_conflict",
  });
});

it.each([
  "destination_uuid",
  "source_uuid",
  "signature",
  "uuid",
  "reason",
  "origin",
  "accepted_identifier_conflicts",
] as const)("rejects a receipt with a changed %s", async (field) => {
  const result = receipt(input());
  const changed = {
    ...result,
    consolidation: {
      ...result.consolidation,
      [field]:
        field === "accepted_identifier_conflicts"
          ? true
          : field === "signature"
            ? "b".repeat(64)
            : field === "origin"
              ? "migration"
              : field === "reason"
                ? "changed"
                : ids.performer,
    },
  };
  const api = createAccountConsolidationAPI(endpoint, async () =>
    Response.json(changed),
  );
  await expect(api.receipt(JSON.stringify(input()))).rejects.toMatchObject({
    code: "receipt_mismatch",
  });
});

it("rejects swapped accounts, hidden conflicts and mismatched owners in previews", () => {
  const original = preview();
  for (const changed of [
    { ...original, source: original.destination, destination: original.source },
    {
      ...original,
      destination: {
        ...original.destination,
        namespace: "mirror:coomer:reddit",
      },
    },
    {
      ...original,
      destination: { ...original.destination, canonical_uuid: ids.performer },
    },
    {
      ...original,
      identifier_conflicts: [
        { namespace: "native:reddit", kind: "id", values: ["a", "b"] },
      ],
    },
    {
      ...original,
      performer: { ...original.performer, uuid: ids.otherPerformer },
    },
    { ...original, ready: false },
  ])
    expect(consolidationPreviewSchema.safeParse(changed).success).toBe(false);
});

it("binds preview responses to the requested choice", async () => {
  const api = createAccountConsolidationAPI(endpoint, async () =>
    Response.json(preview(true)),
  );
  await expect(api.preview(preview().input)).rejects.toMatchObject({
    code: "preview_mismatch",
  });
});

it("checks saved request sizes and conflicting ownership fields before transmission", async () => {
  const transport = vi.fn<typeof fetch>();
  const api = createAccountConsolidationAPI(endpoint, transport);
  await expect(
    api.applySaved(JSON.stringify({ ...input(), ownership_mode: "preserve" })),
  ).rejects.toThrow();
  await expect(api.receipt(" ".repeat(16385))).rejects.toMatchObject({
    code: "request_too_large",
  });
  expect(transport).not.toHaveBeenCalled();
});
