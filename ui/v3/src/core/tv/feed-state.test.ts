import { ApolloClient, InMemoryCache } from "@apollo/client";
import { MockLink, type MockedResponse } from "@apollo/client/testing";
import { describe, expect, it, vi } from "vitest";
import * as GQL from "../generated-graphql";
import { TvFeedController } from "./feed-state";
import type { TvFeedQuery } from "./feed-query";

const query: TvFeedQuery = {
  seed: 1,
  mode: "scenes",
  filter: { sort: "title" },
  pageSize: 5,
  prefetch: 2,
};
const scene = (id: number): GQL.TvSceneSummaryFragment => ({
  __typename: "Scene",
  id: String(id),
  title: `Scene ${id}`,
  paths: { screenshot: null },
  preview_image: null,
});
function fixture(repeated = false, delay = 0) {
  const result = vi.fn((variables: GQL.TvScenesQueryVariables) => ({
    data: {
      findScenes: {
        count: 100,
        scenes: Array.from({ length: 5 }, (_, index) =>
          scene(
            (repeated ? 0 : ((variables.filter?.page ?? 1) - 1) * 5) +
              index +
              1,
          ),
        ),
      },
    },
  }));
  const mock: MockedResponse<GQL.TvScenesQuery, GQL.TvScenesQueryVariables> = {
    request: { query: GQL.TvScenesDocument, variables: () => true },
    result,
    delay,
    maxUsageCount: Infinity,
  };
  const client = new ApolloClient({
    cache: new InMemoryCache(),
    link: new MockLink([mock]),
  });
  return { controller: new TvFeedController(client, query), result };
}

describe("TV feed lifecycle", () => {
  it("starts at an effect boundary, prefetches once, and retains selection", async () => {
    const { controller, result } = fixture();
    expect(result).not.toHaveBeenCalled();
    controller.start();
    await vi.waitFor(() =>
      expect(controller.getSnapshot().items).toHaveLength(5),
    );
    controller.select(1);
    controller.select(1);
    await vi.waitFor(() =>
      expect(controller.getSnapshot().items).toHaveLength(10),
    );
    expect(controller.getSnapshot().selected).toBe(2);
    expect(result).toHaveBeenCalledTimes(2);
    controller.dispose();
  });

  it("ignores late responses after disposal and can restart after effect replay", async () => {
    const { controller } = fixture(false, 20);
    const publish = vi.fn();
    const unsubscribe = controller.subscribe(publish);
    controller.start();
    controller.dispose();
    const published = publish.mock.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(publish).toHaveBeenCalledTimes(published);
    expect(controller.getSnapshot().items).toHaveLength(0);
    controller.start();
    await vi.waitFor(() =>
      expect(controller.getSnapshot().items).toHaveLength(5),
    );
    controller.dispose();
    unsubscribe();
  });

  it("bounds duplicate-only refill work and exposes an explicit continuation", async () => {
    const { controller, result } = fixture(true);
    controller.start();
    await vi.waitFor(() =>
      expect(controller.getSnapshot().items).toHaveLength(5),
    );
    controller.select(1);
    controller.select(1);
    await vi.waitFor(() =>
      expect(controller.getSnapshot().status).toBe("continue"),
    );
    expect(result).toHaveBeenCalledTimes(4);
    expect(controller.getSnapshot().items).toHaveLength(5);
    controller.dispose();
  });

  it("resolves an item link after paging and prevents deleted IDs returning", async () => {
    const { controller } = fixture();
    controller.selectKey("scene:8");
    controller.start();
    await vi.waitFor(() =>
      expect(
        controller.getSnapshot().items[controller.getSnapshot().selected]?.id,
      ).toBe("8"),
    );
    const current =
      controller.getSnapshot().items[controller.getSnapshot().selected];
    if (!current) throw new Error("Missing current selection");
    controller.reconcile(current);
    await vi.waitFor(() =>
      expect(controller.getSnapshot().status).not.toBe("loading"),
    );
    expect(controller.getSnapshot().items.some((item) => item.id === "8")).toBe(
      false,
    );
    expect(controller.getSnapshot().tombstones).toContain("scene:8");
    controller.dispose();
  });
});
