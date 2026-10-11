import { describe, expect, it, vi } from "vitest";
import { createScrapeMetricsAPI } from "./scrape-metrics-api";

const metrics = {
  since: "2026-10-10T00:00:00Z",
  metrics: [
    {
      source_scope: "service:twitter",
      class: "routine",
      operation: "listing",
      requests: 10,
      responses: 9,
      rate_limited: 1,
      errors: 1,
      new_posts: 0,
      repeated_posts: 0,
    },
  ],
  accounts: [],
  total: 113,
};
describe("scrape request metrics", () => {
  it("loads exact account totals and the requested page", async () => {
    const transport = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify(metrics)));
    const api = createScrapeMetricsAPI(
      "https://stash.example/api/v3/archive/",
      transport,
    );
    const result = await api(50);
    expect(result.total).toBe(113);
    expect(result.metrics[0]?.rate_limited).toBe(1);
    expect(String(transport.mock.calls[0]?.[0])).toContain("offset=50");
  });
  it("rejects invalid counters instead of displaying misleading totals", async () => {
    const transport = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ ...metrics, total: -1 })),
      );
    await expect(
      createScrapeMetricsAPI(
        "https://stash.example/api/v3/archive/",
        transport,
      )(0),
    ).rejects.toThrow();
  });
});
