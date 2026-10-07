import { expect, it, vi } from "vitest";
import {
  createImportHistoryAPI,
  importHistorySearchSchema,
} from "./import-history-api";
import {
  importDetails,
  importIds,
  importSnapshot,
} from "../../../tests/fixtures/import-history";

function client(body: unknown) {
  const transport = vi.fn<typeof fetch>(async () => Response.json(body));
  return {
    api: createImportHistoryAPI(
      "https://example.test/stash/api/v3/archive/",
      transport,
    ),
    transport,
  };
}

it("reads bounded snapshots through the application session without submitting work", async () => {
  const row = { ...importSnapshot(), uuid: importIds.later };
  const { api, transport } = client([row]);
  expect(await api.snapshots("catalog", importIds.snapshot)).toEqual([row]);
  const [address, options] = transport.mock.calls[0] ?? [];
  const url = new URL(String(address));
  expect(url.pathname).toBe("/stash/api/v3/archive/import-history/catalog");
  expect(Object.fromEntries(url.searchParams)).toEqual({
    limit: "25",
    after: importIds.snapshot,
  });
  expect(options).toMatchObject({
    method: "GET",
    cache: "no-store",
    credentials: "same-origin",
  });
  expect(options?.body).toBeUndefined();
  expect(importHistorySearchSchema.parse({})).toEqual({ kind: "catalog" });
});

it("rejects duplicates, reversed cursors, wrong kinds and oversized pages", async () => {
  const first = importSnapshot();
  const later = { ...first, uuid: importIds.later };
  expect(await client([first, later]).api.snapshots("catalog")).toHaveLength(2);
  for (const rows of [
    [first, first],
    [later, first],
    [importSnapshot("automation")],
    Array.from({ length: 26 }, () => first),
  ])
    await expect(client(rows).api.snapshots("catalog")).rejects.toThrow();
  await expect(
    client([first]).api.snapshots("catalog", first.uuid),
  ).rejects.toThrow();
});

it("does not accept received data with missing bytes or records", async () => {
  const row = importSnapshot();
  for (const changed of [
    { received_bytes: 1 },
    { received_records: 1 },
    { received_chunks: 1 },
    { transfer_state: "receiving" },
    { received_bytes: row.bytes + 1 },
    { collection_uuid: null },
    { chunks: -1 },
  ])
    await expect(
      client([{ ...row, ...changed }]).api.snapshots("catalog"),
    ).rejects.toThrow();
  expect(
    await client([
      {
        ...row,
        transfer_state: "receiving",
        received_chunks: 1,
        received_bytes: 1600,
        received_records: 40,
      },
    ]).api.snapshots("catalog"),
  ).toHaveLength(1);
  expect(
    await client([
      {
        ...importSnapshot("automation"),
        chunks: 0,
        received_chunks: 0,
        bytes: 0,
        received_bytes: 0,
        records: 0,
        received_records: 0,
      },
    ]).api.snapshots("automation"),
  ).toHaveLength(1);
});

it("preserves unstarted families and historical warnings without claiming review completion", async () => {
  const detail = importDetails();
  const result = await client({
    ...detail,
    imported: true,
    settings: { private: true },
  }).api.snapshot("catalog", importIds.snapshot);
  expect(result).toEqual(detail);
  expect(result.families[0]?.progress?.historical_review_records).toBe(2);
  expect(result.families[1]?.progress).toBeNull();
  expect(
    await client(importDetails("automation")).api.snapshot(
      "automation",
      importIds.snapshot,
    ),
  ).toEqual(importDetails("automation"));
  await expect(
    client(detail).api.snapshot("catalog", importIds.later),
  ).rejects.toThrow();
  await expect(
    client(detail).api.snapshot("automation", importIds.snapshot),
  ).rejects.toThrow();
  for (const families of [
    detail.families.slice(1),
    [...detail.families].reverse(),
    [...detail.families, detail.families[0]],
  ])
    await expect(
      client({ ...detail, families }).api.snapshot(
        "catalog",
        importIds.snapshot,
      ),
    ).rejects.toThrow();
});

it("rejects false terminal progress and malformed requests before transport", async () => {
  const detail = importDetails();
  const progress = detail.families[0]?.progress;
  for (const change of [
    { processed_records: 3 },
    { historical_review_records: 6 },
    { state: "mapped" },
    { state: "done" },
  ]) {
    const families = detail.families.map((family, index) =>
      index === 0
        ? { ...family, progress: { ...progress, ...change } }
        : family,
    );
    await expect(
      client({ ...detail, families }).api.snapshot(
        "catalog",
        importIds.snapshot,
      ),
    ).rejects.toThrow();
  }
  const { api, transport } = client([]);
  await expect(api.snapshots("catalog", "bad")).rejects.toThrow();
  await expect(api.snapshot("automation", "bad")).rejects.toThrow();
  expect(transport).not.toHaveBeenCalled();
});
