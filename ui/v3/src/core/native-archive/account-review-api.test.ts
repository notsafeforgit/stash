import { expect, it, vi } from "vitest";
import {
  createAccountReviewAPI,
  accountFilterSchema,
  ownershipInputSchema,
  ownershipRequestKey,
} from "./account-review-api";
import { nativeArchiveEndpoint } from "./client";
import {
  account,
  ids,
  preview,
  receipt,
} from "../../../tests/fixtures/account-review";

const endpoint = nativeArchiveEndpoint(
  new URL("https://example.test/stash/?private=value#hash"),
);
const filter = accountFilterSchema.parse({});
const apply = {
  ...preview().input,
  digest: preview().digest,
  request_uuid: ids.request,
};

it("uses bounded, scoped account and evidence lookups with session requests and cancellation", async () => {
  const transport = vi.fn<typeof fetch>(async () => Response.json([]));
  const api = createAccountReviewAPI(endpoint, transport);
  const controller = new AbortController();
  await api.accounts(
    {
      ...filter,
      q: "river%_",
      namespace: "native:reddit",
      ownership: "linked",
    },
    ids.otherAccount,
    controller.signal,
  );
  await api.identifiers(ids.account, ids.handle);
  await api.evidence(ids.handle, "capture:first");
  await api.history(ids.account, 7);
  await api.accounts({ ...filter, ownership: "all" });
  expect(endpoint).toBe("https://example.test/stash/api/v3/archive/");
  const urls = transport.mock.calls.map(([url]) => new URL(String(url)));
  expect(Object.fromEntries(urls[0]?.searchParams ?? [])).toEqual({
    limit: "25",
    q: "river%_",
    namespace: "native:reddit",
    scope: "tracked",
    ownership: "linked",
    after: ids.otherAccount,
  });
  expect(urls[1]?.pathname).toBe(
    `/stash/api/v3/archive/source-accounts/${ids.account}/identifiers`,
  );
  expect(Object.fromEntries(urls[1]?.searchParams ?? [])).toEqual({
    limit: "25",
    after: ids.handle,
  });
  expect(urls[2]?.pathname).toBe(
    `/stash/api/v3/archive/source-account-identifiers/${ids.handle}/evidence`,
  );
  expect(urls[2]?.searchParams.get("after")).toBe("capture:first");
  expect(urls[3]?.searchParams.get("after")).toBe("7");
  expect(urls[4]?.searchParams.has("ownership")).toBe(false);
  expect(urls[4]?.searchParams.get("scope")).toBe("tracked");
  await api.accounts({ ...filter, scope: "all" });
  expect(
    new URL(String(transport.mock.calls[5]?.[0])).searchParams.get("scope"),
  ).toBe("all");
  expect(transport.mock.calls[0]?.[1]).toMatchObject({
    method: "GET",
    credentials: "same-origin",
    cache: "no-store",
    redirect: "error",
    signal: controller.signal,
  });
});

it("binds selected accounts and performer identities to the requested target", async () => {
  const api = createAccountReviewAPI(endpoint, async (url) =>
    Response.json(
      String(url).includes("entity-identities")
        ? { uuid: ids.performer, kind: "performer", revision: 3, local_id: 11 }
        : { ...account(), uuid: ids.otherAccount },
    ),
  );
  await expect(api.account(ids.account)).rejects.toMatchObject({
    code: "account_mismatch",
  });
  await expect(api.performer("10")).rejects.toMatchObject({
    code: "identity_mismatch",
  });
  await expect(api.performer("../10")).rejects.toMatchObject({
    code: "invalid_identity",
  });
});

it("rejects previews with another account, changed revisions, a missing owner or a different explicit choice", async () => {
  const original = preview();
  const wrong = [
    {
      ...original,
      input: { ...original.input, performer_uuid: ids.otherPerformer },
    },
    { ...original, account: { ...original.account, uuid: ids.otherAccount } },
    {
      ...original,
      account: { ...original.account, canonical_uuid: ids.otherAccount },
    },
    { ...original, account: { ...original.account, revision: 3 } },
    { ...original, performer: undefined },
    { ...original, performer: { ...original.performer, revision: 4 } },
    { ...original, performer: { ...original.performer, state: "deleted" } },
  ];
  for (const value of wrong) {
    const api = createAccountReviewAPI(endpoint, async () =>
      Response.json(value),
    );
    await expect(api.preview(original.input)).rejects.toMatchObject({
      code: "preview_mismatch",
    });
  }
  const transport = vi.fn<typeof fetch>(async () => Response.json(original));
  await createAccountReviewAPI(endpoint, transport).preview({
    ...original.input,
    reason: "",
  });
  expect(JSON.parse(String(transport.mock.calls[0]?.[1]?.body))).toEqual(
    original.input,
  );
});

it("requires explicit linked targets and keeps internal settings and unrelated edits out of requests", async () => {
  const transport = vi.fn<typeof fetch>();
  const api = createAccountReviewAPI(endpoint, transport);
  for (const input of [
    { account_uuid: ids.account, account_revision: 2, state: "linked" },
    { ...preview().input, state: "unlinked" },
    { ...preview().input, settings: {} },
    { ...preview().input, reason: "bad\nreason" },
    { ...preview().input, reason: "猫".repeat(1366) },
  ])
    expect(ownershipInputSchema.safeParse(input).success).toBe(false);
  expect(
    ownershipInputSchema.safeParse({
      ...preview().input,
      reason: "猫".repeat(1365),
    }).success,
  ).toBe(true);
  expect(accountFilterSchema.safeParse({ q: "猫".repeat(86) }).success).toBe(
    false,
  );
  expect(accountFilterSchema.safeParse({ q: "a\t" }).success).toBe(false);
  await expect(
    api.applySaved(JSON.stringify({ ...apply, settings: {} })),
  ).rejects.toThrow();
  await expect(api.applySaved(" ".repeat(16385))).rejects.toMatchObject({
    code: "request_too_large",
  });
  expect(transport).not.toHaveBeenCalled();
});

it("validates receipts against the original request, preserving exact saved bytes on retry", async () => {
  const body = JSON.stringify({ ...apply, reason: "" }, null, 2);
  const transport = vi.fn<typeof fetch>(async () =>
    Response.json({ review: receipt(apply), replayed: true }),
  );
  const result = await createAccountReviewAPI(endpoint, transport).applySaved(
    body,
  );
  expect(result.replayed).toBe(true);
  expect(transport.mock.calls[0]?.[1]?.body).toBe(body);
  expect(ownershipRequestKey(apply)).toBe(
    ownershipRequestKey({ ...apply, reason: "" }),
  );
  const wrong = createAccountReviewAPI(endpoint, async () =>
    Response.json(receipt({ ...apply, performer_uuid: ids.otherPerformer })),
  );
  await expect(wrong.receipt(apply)).rejects.toMatchObject({
    code: "receipt_mismatch",
  });
  const missing = createAccountReviewAPI(endpoint, async () =>
    Response.json({ error: "not_found" }, { status: 404 }),
  );
  expect(await missing.receipt(apply)).toBeNull();
  const unavailable = createAccountReviewAPI(endpoint, async () =>
    Response.json({ error: "unavailable" }, { status: 503 }),
  );
  await expect(unavailable.receipt(apply)).rejects.toMatchObject({
    status: 503,
  });
});

it("does not accept unbounded account pages", async () => {
  const api = createAccountReviewAPI(endpoint, async () =>
    Response.json(Array.from({ length: 26 }, account)),
  );
  await expect(api.accounts(filter)).rejects.toMatchObject({
    code: "invalid_response",
  });
});
