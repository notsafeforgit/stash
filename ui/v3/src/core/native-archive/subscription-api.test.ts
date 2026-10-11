import { expect, it, vi } from "vitest";
import {
  createSubscriptionAPI,
  type SourceSubscription,
} from "./subscription-api";

const id = "00000000-0000-4000-8000-000000000001";
const item: SourceSubscription = {
  enabled: true,
  revision: 1,
  collection: {
    uuid: id,
    revision: 1,
    created_at: "2026-10-11T00:00:00Z",
    label: "Example profile",
    namespace: "native:reddit",
    kind: "account",
    state: "active",
    account_uuid: null,
    root_uuid: id,
    path_prefix: ".",
    target_url: "https://reddit.com/user/example/",
  },
};
const endpoint = "https://example.test/stash/api/v3/archive/";

it("stops monitoring with an exact subscription revision and recovers a lost response", async () => {
  const transport = vi
    .fn<typeof fetch>()
    .mockRejectedValueOnce(new TypeError("lost response"))
    .mockResolvedValueOnce(
      Response.json({
        items: [{ ...item, enabled: false, revision: 2 }],
        total: 1,
      }),
    );
  const api = createSubscriptionAPI(endpoint, transport);
  expect(await api.save(item, false)).toMatchObject({
    enabled: false,
    revision: 2,
  });
  const [url, options] = transport.mock.calls[0]!;
  expect(String(url)).toBe(`${endpoint}collections/${id}/subscription`);
  expect(options).toMatchObject({ method: "PUT", credentials: "same-origin" });
  expect(JSON.parse(String(options?.body))).toEqual({
    expected_revision: 1,
    enabled: false,
  });
  expect(transport).toHaveBeenCalledTimes(2);
});

it("does not claim success after a later conflicting owner edit", async () => {
  const transport = vi
    .fn<typeof fetch>()
    .mockRejectedValueOnce(new TypeError("lost response"))
    .mockResolvedValueOnce(
      Response.json({
        items: [{ ...item, enabled: false, revision: 4 }],
        total: 1,
      }),
    );
  await expect(
    createSubscriptionAPI(endpoint, transport).save(item, false),
  ).rejects.toThrow();
});

it("keeps bulk stop scoped to the performer and uses counts for pagination", async () => {
  const transport = vi
    .fn<typeof fetch>()
    .mockResolvedValueOnce(Response.json({ items: [item], total: 26 }))
    .mockResolvedValueOnce(Response.json({ stopped: 2 }));
  const api = createSubscriptionAPI(endpoint, transport);
  expect((await api.list("performer", id, 2)).total).toBe(26);
  expect(
    new URL(String(transport.mock.calls[0]![0])).searchParams.get("offset"),
  ).toBe("25");
  await api.stopPerformer(id);
  expect(String(transport.mock.calls[1]![0])).toBe(
    `${endpoint}entities/${id}/stop-monitoring`,
  );
});
