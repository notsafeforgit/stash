import { expect, it, vi } from "vitest";
import {
  createMetadataReviewAPI,
  metadataReviewEndpoint,
} from "./metadata-review-api";
import { ids } from "../../../tests/fixtures/metadata-review";

it("uses the application mount with same-origin session requests and passes cancellation", async () => {
  const endpoint = metadataReviewEndpoint(
    new URL("https://example.test/stash/?unrelated=secret#fragment"),
  );
  expect(endpoint).toBe("https://example.test/stash/api/v3/archive/");
  const transport = vi.fn<typeof fetch>(async () =>
    Response.json({
      uuid: ids.entity,
      revision: 2,
      kind: "scene",
    }),
  );
  const api = createMetadataReviewAPI(endpoint, transport);
  const controller = new AbortController();
  await api.identity("scene", "7", controller.signal);
  expect(String(transport.mock.calls[0]?.[0])).toBe(
    `${endpoint}entity-identities/scene/7`,
  );
  expect(transport.mock.calls[0]?.[1]).toMatchObject({
    credentials: "same-origin",
    cache: "no-store",
    signal: controller.signal,
    redirect: "error",
  });
  expect(() =>
    metadataReviewEndpoint(new URL("https://user:secret@example.test/")),
  ).toThrow();
});

it.each([{ uuid: ids.entity, kind: "scene", local_id: 7, revision: 2 }])(
  "rejects invalid resolved performer identities: %j",
  async (response) => {
    const api = createMetadataReviewAPI(
      "https://example.test/api/v3/archive/",
      async () => Response.json(response),
    );
    await expect(api.identity("performer", ids.entity)).rejects.toMatchObject({
      code: "identity_mismatch",
    });
  },
);

it("bounds decision history pages and keeps the field name in its path segment", async () => {
  const transport = vi.fn<typeof fetch>(async () => Response.json([]));
  const api = createMetadataReviewAPI(
    "https://example.test/api/v3/archive/",
    transport,
  );
  await api.decisions(ids.entity, "custom/fields", 25);
  const url = new URL(String(transport.mock.calls[0]?.[0]));
  expect(url.pathname).toBe(
    `/api/v3/archive/entities/${ids.entity}/metadata-fields/custom%2Ffields/history`,
  );
  expect(Object.fromEntries(url.searchParams)).toEqual({
    after: "25",
    limit: "25",
  });
  expect(() => api.decisions(ids.entity, "title", -1)).toThrow();
  expect(() => api.decisions("not-a-uuid", "title")).toThrow();
  expect(transport).toHaveBeenCalledTimes(1);
});
