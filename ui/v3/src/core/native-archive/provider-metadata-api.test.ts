import { expect, it, vi } from "vitest";
import { createProviderMetadataAPI } from "./provider-metadata-api";

const id = "11111111-1111-4111-8111-111111111111";
const receipt = {
  sequence: 4,
  uuid: "22222222-2222-4222-8222-222222222222",
  entity_uuid: id,
  original_entity_uuid: id,
  entity_kind: "performer",
  entity_revision: 3,
  endpoint: "https://provider.test/graphql",
  remote_id: "person-7",
  operation: "review",
  values: { name: "Imported name", aliases: ["Local", "Remote"] },
  signature: "a".repeat(64),
  created_at: "2026-10-08T00:00:00Z",
};
function client(body: unknown) {
  const transport = vi.fn<typeof fetch>(async () => Response.json(body));
  return {
    transport,
    api: createProviderMetadataAPI(
      "https://stash.test/mount/api/v3/archive/",
      transport,
    ),
  };
}

it("reads a bounded history page through the application session", async () => {
  const { api, transport } = client([receipt]);
  expect(await api.history(id, "performer", 3)).toEqual([receipt]);
  const [address, options] = transport.mock.calls[0] ?? [];
  const url = new URL(String(address));
  expect(url.pathname).toBe(
    `/mount/api/v3/archive/entities/${id}/provider-metadata-history`,
  );
  expect(Object.fromEntries(url.searchParams)).toEqual({
    after: "3",
    limit: "25",
  });
  expect(options).toMatchObject({
    method: "GET",
    credentials: "same-origin",
    cache: "no-store",
  });
  expect(options?.body).toBeUndefined();
});

it("rejects identity mixups, malformed evidence and repeated or reversed pages", async () => {
  for (const rows of [
    [{ ...receipt, entity_uuid: receipt.uuid }],
    [{ ...receipt, entity_kind: "scene" }],
    [{ ...receipt, values: {} }],
    [{ ...receipt, endpoint: "https://user:secret@provider.test/graphql" }],
    [{ ...receipt, signature: "not-a-digest" }],
    [receipt, receipt],
    [receipt, { ...receipt, sequence: 3 }],
    Array.from({ length: 26 }, () => receipt),
  ])
    await expect(client(rows).api.history(id, "performer")).rejects.toThrow();
  await expect(
    client([receipt]).api.history(id, "performer", 4),
  ).rejects.toThrow();
  const { api, transport } = client([]);
  await expect(api.history("not-a-uuid", "scene")).rejects.toThrow();
  await expect(api.history(id, "scene", -1)).rejects.toThrow();
  expect(transport).not.toHaveBeenCalled();
});

it("verifies the library identity before reading its history", async () => {
  const identity = { uuid: id, kind: "scene", revision: 1, local_id: 7 };
  expect(await client(identity).api.identity("scene", "7")).toEqual(identity);
  await expect(
    client({ ...identity, kind: "performer" }).api.identity("scene", "7"),
  ).rejects.toThrow();
  await expect(
    client({ ...identity, local_id: 8 }).api.identity("scene", "7"),
  ).rejects.toThrow();
});
