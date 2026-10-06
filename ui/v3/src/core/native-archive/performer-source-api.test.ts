import { expect, it, vi } from "vitest";
import { createPerformerSourceAPI } from "./performer-source-api";
import { account, ids } from "../../../tests/fixtures/account-review";

const endpoint = "https://example.test/stash/api/v3/archive/";
const former = "00000000-0000-4000-8000-000000000090";
function fixture() {
  const value = account();
  value.ownership = {
    decision_uuid: "00000000-0000-4000-8000-000000000091",
    state: "linked",
    revision: 1,
    performer_uuid: former,
    performer: {
      uuid: ids.performer,
      revision: 2,
      state: "active",
      local_id: 7,
      name: "Canonical performer",
    },
    origin: "review",
    reason: "Confirmed owner",
    created_at: "2026-10-01T00:00:00Z",
  };
  return {
    requested_uuid: former,
    performer: value.ownership.performer!,
    accounts: [value],
  };
}
function client(value: unknown) {
  const transport = vi.fn<typeof fetch>(async () => Response.json(value));
  return { api: createPerformerSourceAPI(endpoint, transport), transport };
}

it("uses a scoped session read and preserves an owner selected before a performer merge", async () => {
  const page = fixture();
  const { api, transport } = client(page);
  expect(await api.accounts(former)).toEqual(page);
  const [url, options] = transport.mock.calls[0] ?? [];
  expect(new URL(String(url)).pathname).toBe(
    `/stash/api/v3/archive/entities/${former}/source-accounts`,
  );
  expect(options).toMatchObject({
    method: "GET",
    credentials: "same-origin",
    redirect: "error",
  });
  expect(options?.body).toBeUndefined();
});

it("rejects unlinked, unrelated and redirected account rows", async () => {
  const valid = fixture();
  for (const changed of [
    { ...valid.accounts[0]!, ownership: undefined },
    {
      ...valid.accounts[0]!,
      ownership: { ...valid.accounts[0]!.ownership!, state: "unlinked" },
    },
    { ...valid.accounts[0]!, redirect_to: former },
    {
      ...valid.accounts[0]!,
      ownership: {
        ...valid.accounts[0]!.ownership!,
        performer: { ...valid.performer, uuid: former },
      },
    },
  ]) {
    await expect(
      client({ ...valid, accounts: [changed] }).api.accounts(former),
    ).rejects.toThrow();
  }
});

it("validates request scope, page ordering, cursor and size", async () => {
  const valid = fixture();
  await expect(client(valid).api.accounts(ids.performer)).rejects.toThrow();
  await expect(
    client({
      ...valid,
      accounts: [valid.accounts[0], valid.accounts[0]],
    }).api.accounts(former),
  ).rejects.toThrow();
  await expect(
    client(valid).api.accounts(former, valid.accounts[0]!.uuid),
  ).rejects.toThrow();
  await expect(
    client({
      ...valid,
      accounts: Array(26).fill(valid.accounts[0]),
    }).api.accounts(former),
  ).rejects.toThrow();
  const { api, transport } = client(valid);
  await expect(api.accounts("1")).rejects.toThrow();
  expect(transport).not.toHaveBeenCalled();
});

it("retains deletion without a live local ID", async () => {
  const valid = fixture();
  const retired = { ...valid.performer, state: "deleted", local_id: undefined };
  const page = {
    ...valid,
    performer: retired,
    accounts: valid.accounts.map((row) => ({
      ...row,
      ownership: { ...row.ownership, performer: retired },
    })),
  };
  expect(
    (await client(page).api.accounts(former)).performer.local_id,
  ).toBeUndefined();
  await expect(
    client({ ...page, performer: { ...retired, local_id: 7 } }).api.accounts(
      former,
    ),
  ).rejects.toThrow();
});

it("validates redirect history independently and retains historical local IDs as evidence", async () => {
  const valid = fixture();
  const page = {
    requested_uuid: former,
    performer: valid.performer,
    identities: [
      {
        uuid: former,
        revision: 2,
        state: "redirected",
        original_id: 72,
        redirect_to: valid.performer.uuid,
        created_at: "2026-09-01T00:00:00Z",
        retired_at: "2026-10-01T00:00:00Z",
      },
    ],
  };
  const { api, transport } = client(page);
  expect((await api.identities(former)).identities[0]?.original_id).toBe(72);
  expect(String(transport.mock.calls[0]?.[0])).toContain(
    "/performer-identities?limit=25",
  );
  for (const change of [
    { state: "active", retired_at: null, redirect_to: null },
    { retired_at: null },
    { redirect_to: former },
  ]) {
    await expect(
      client({
        ...page,
        identities: [{ ...page.identities[0], ...change }],
      }).api.identities(former),
    ).rejects.toThrow();
  }
});
