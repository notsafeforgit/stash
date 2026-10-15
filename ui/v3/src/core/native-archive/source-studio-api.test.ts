import { describe, expect, it, vi } from "vitest";
import { createSourceStudioAPI } from "./source-studio-api";

const collection = "00000000-0000-4000-8000-000000000001";
const studio = {
  uuid: "00000000-0000-4000-8000-000000000002",
  name: "Subreddit",
};

describe("source studios", () => {
  it("saves a studio or explicit clear with the revision that was reviewed", async () => {
    const current = { collection_uuid: collection, revision: 4, studio };
    const transport = vi
      .fn<typeof fetch>()
      .mockResolvedValue(
        new Response(JSON.stringify({ ...current, revision: 5, studio: null })),
      );
    const api = createSourceStudioAPI(
      "https://stash.test/api/v3/archive/",
      transport,
    );
    await expect(api.save(current, null)).resolves.toMatchObject({
      revision: 5,
      studio: null,
    });
    expect(transport.mock.calls[0]?.[0].toString()).toBe(
      `https://stash.test/api/v3/archive/collections/${collection}/studio`,
    );
    expect(transport.mock.calls[0]?.[1]).toMatchObject({
      method: "PUT",
      body: JSON.stringify({ expected_revision: 4, studio_uuid: null }),
      credentials: "same-origin",
    });
  });

  it("retains every contributing studio and surfaces conflicts", async () => {
    const other = { ...studio, uuid: "00000000-0000-4000-8000-000000000003" };
    const transport = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(new Response(JSON.stringify([studio, other])))
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ error: "preview_changed" }), {
          status: 409,
        }),
      );
    const api = createSourceStudioAPI(
      "https://stash.test/api/v3/archive/",
      transport,
    );
    expect(await api.post(collection)).toEqual([studio, other]);
    await expect(
      api.save(
        { collection_uuid: collection, revision: 0, studio: null },
        studio,
      ),
    ).rejects.toMatchObject({ status: 409 });
  });
});
