// @vitest-environment jsdom

import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { resolvePerformerRoute } from "./performer-route";

const canonical = "00000000-0000-4000-8000-000000000002";
const former = "00000000-0000-4000-8000-000000000001";

beforeEach(() => {
  vi.stubEnv("NODE_ENV", "development");
  document.head.innerHTML = '<base href="/stash/">';
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  document.head.innerHTML = "";
});

it.each(["807", former, canonical])(
  "opens %s at its canonical UUID with filters, tab, hash and browsing origin intact",
  async (id) => {
    const transport = vi.fn<typeof fetch>(async () =>
      Response.json({
        uuid: canonical,
        kind: "performer",

        revision: 2,
      }),
    );
    vi.stubGlobal("fetch", transport);
    const root = createRootRoute();
    const detail = createRoute({
      getParentRoute: () => root,
      path: "/performers/$performerId",
      validateSearch: (search: Record<string, unknown>) => search,
      loader: ({ params, location, abortController }) =>
        resolvePerformerRoute(
          params.performerId,
          location,
          abortController.signal,
        ),
    });
    const history = createMemoryHistory({
      initialEntries: ["/stash/performers"],
    });
    const returnTo = "/performers?q=example&p=2";
    history.push(`/stash/performers/${id}?tab=images&p=3#list`, { returnTo });
    const router = createRouter({
      routeTree: root.addChildren([detail]),
      history,
      basepath: "/stash",
    });
    await router.load();
    await vi.waitFor(() => {
      expect(history.location.href).toBe(
        `/stash/performers/${canonical}?tab=images&p=3#list`,
      );
      expect(router.state.matches.at(-1)?.loaderData).toBe(canonical);
    });
    expect(router.state.location.state.returnTo).toBe(returnTo);
    expect(
      transport.mock.calls.every(([url]) =>
        String(url).includes(
          "/stash/api/v3/archive/entity-identities/performer/",
        ),
      ),
    ).toBe(true);
    history.back();
    expect(history.location.href).toBe("/stash/performers");
  },
);

it("shows missing performers without falling back to a numeric key and surfaces server failures", async () => {
  const location = { search: {}, hash: "", state: { __TSR_index: 0 } };
  vi.stubGlobal("fetch", async () =>
    Response.json({ error: "not_found" }, { status: 404 }),
  );
  expect(await resolvePerformerRoute(canonical, location)).toBeNull();
  vi.stubGlobal("fetch", async () =>
    Response.json({ error: "unavailable" }, { status: 503 }),
  );
  await expect(
    resolvePerformerRoute(canonical, location),
  ).rejects.toMatchObject({ status: 503 });
});
