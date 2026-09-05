import { describe, expect, it } from "vitest";
import {
  createRootRoute,
  createRoute,
  createRouter,
  createMemoryHistory,
} from "@tanstack/react-router";
import {
  applicationBaseURL,
  applicationHref,
  applicationPath,
  joinPlatformURL,
} from "./platform-url";

describe("public mount point", () => {
  it("preserves encoded filters in links and raw pagination history", async () => {
    const base = applicationBaseURL("https://example.test", "/stash/");
    const filter = "/scenes?c=%7B%22value%22%3A%22a%2Fb%22%7D&p=2#top";
    expect(applicationHref(filter, base)).toBe(`/stash${filter}`);
    const history = createMemoryHistory({ initialEntries: ["/stash/scenes"] });
    history.push(applicationHref(filter, base));
    expect(history.location.href).toBe(`/stash${filter}`);
    expect(applicationPath(history.location.href, base)).toBe(filter);
    history.back();
    expect(history.location.href).toBe("/stash/scenes");
  });
  it.each([
    "/",
    "/stash",
    "/stash/",
    "/apps/stash/",
  ])("joins API, assets, and downloads below %s", (prefix) => {
    const base = applicationBaseURL("https://example.test", prefix);
    const normalized = `${prefix.replace(/\/+$/, "")}/`;
    for (const path of [
      "graphql",
      "/css",
      "javascript",
      "scene/12/download.mp4?mode=ORIGINAL&x=1",
    ]) {
      expect(joinPlatformURL(base, path).href).toBe(
        `https://example.test${normalized}${path.replace(/^\//, "")}`,
      );
    }
    expect(applicationPath(`${base}scenes?sort=date#top`, base)).toBe(
      "/scenes?sort=date#top",
    );
  });

  it("does not strip a different route prefix", () => {
    expect(
      applicationPath(
        "https://example.test/stash-other",
        applicationBaseURL("https://example.test", "/stash/"),
      ),
    ).toBe("/stash-other");
  });

  it("matches deep links and builds navigation below the mount point", async () => {
    const root = createRootRoute();
    const scenes = createRoute({ getParentRoute: () => root, path: "/scenes" });
    const router = createRouter({
      routeTree: root.addChildren([scenes]),
      basepath: applicationBaseURL("https://example.test", "/stash/").pathname,
      history: createMemoryHistory({ initialEntries: ["/stash/scenes"] }),
    });
    await router.load();
    expect(router.state.matches.at(-1)?.routeId).toBe("/scenes");
    expect(router.state.matches[0]?.globalNotFound).not.toBe(true);
    expect(router.buildLocation({ to: "/scenes" }).href).toBe("/stash/scenes");
  });
});
