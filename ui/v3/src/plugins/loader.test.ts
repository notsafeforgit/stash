import {
  ApolloClient,
  ApolloLink,
  InMemoryCache,
  Observable,
} from "@apollo/client";
import { createIntl } from "react-intl";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { StashPluginHost } from "./host";
import { deferred } from "@/test-utils/deferred";

vi.mock("./ui-exports", () => ({}));

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.resetModules();
});

describe("plugin startup", () => {
  it("shares discovery with configuration prefetch without freezing registration early", async () => {
    const { prefetchPluginMetadata, ensurePluginsLoaded } = await import(
      "./loader"
    );
    const { isRegistryFrozen } = await import("./registry");
    const response = deferred<void>();
    const request = vi.fn(
      () =>
        new Observable<{ data: { plugins: [] } }>((observer) => {
          void response.promise.then(() => {
            observer.next({ data: { plugins: [] } });
            observer.complete();
          });
        }),
    );
    const apollo = new ApolloClient({
      cache: new InMemoryCache(),
      link: new ApolloLink(request),
    });
    const metadata = prefetchPluginMetadata(apollo);
    expect(prefetchPluginMetadata(apollo)).toBe(metadata);
    expect(request).toHaveBeenCalledTimes(1);
    expect(isRegistryFrozen()).toBe(false);
    response.resolve();
    await metadata;
    expect(isRegistryFrozen()).toBe(false);
    expect(
      await ensurePluginsLoaded({ apollo, intl: createIntl({ locale: "en" }) }),
    ).toEqual([]);
    expect(isRegistryFrozen()).toBe(true);
    expect(request).toHaveBeenCalledTimes(1);
    apollo.stop();
  });

  it("reports a failed prefetch after configuration finishes without a duplicate request", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const { prefetchPluginMetadata, ensurePluginsLoaded } = await import(
      "./loader"
    );
    const request = vi.fn(
      () =>
        new Observable<{ data: { plugins: [] } }>((observer) =>
          observer.error(new Error("Offline")),
        ),
    );
    const apollo = new ApolloClient({
      cache: new InMemoryCache(),
      link: new ApolloLink(request),
    });
    await expect(prefetchPluginMetadata(apollo)).rejects.toThrow("Offline");
    expect(
      await ensurePluginsLoaded({ apollo, intl: createIntl({ locale: "en" }) }),
    ).toHaveLength(1);
    expect(request).toHaveBeenCalledTimes(1);
    apollo.stop();
  });

  it("discards a hung plugin's partial and late registrations, then loads a healthy plugin", async () => {
    vi.useFakeTimers();
    const { registerPlugin } = await import("./loader");
    const { getRegisteredNavItems } = await import("./registry");
    const opts = {
      apollo: {} as ApolloClient,
      intl: createIntl({ locale: "en" }),
    };
    let lateHost: StashPluginHost | undefined;
    let finish: (() => void) | undefined;
    const pending = registerPlugin(
      { id: "hung", name: "Hung", entry: "hung" },
      opts,
      100,
      async () => ({
        register: async (host) => {
          lateHost = host;
          host.nav.add({ label: "Partial", to: "/partial" });
          await new Promise<void>((resolve) => {
            finish = resolve;
          });
          host.nav.add({ label: "Late", to: "/late" });
        },
      }),
    );
    const rejection = expect(pending).rejects.toThrow("timed out");
    await vi.advanceTimersByTimeAsync(100);
    await rejection;
    finish?.();
    lateHost?.nav.add({ label: "Too late", to: "/too-late" });
    await registerPlugin(
      { id: "healthy", name: "Healthy", entry: "healthy" },
      opts,
      100,
      async () => ({
        register: (host) => host.nav.add({ label: "Healthy", to: "/healthy" }),
      }),
    );
    expect(getRegisteredNavItems().map((item) => item.label)).toEqual([
      "Healthy",
    ]);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("finishes startup when the plugin-list query never settles", async () => {
    vi.useFakeTimers();
    vi.spyOn(console, "error").mockImplementation(() => {});
    const { ensurePluginsLoaded, PLUGIN_TIMEOUT_MS } = await import("./loader");
    const { isRegistryFrozen } = await import("./registry");
    const query = vi.fn(() => new Promise(() => {}));
    const opts = {
      apollo: { query } as unknown as ApolloClient,
      intl: createIntl({ locale: "en" }),
    };
    const pending = ensurePluginsLoaded(opts);
    expect(ensurePluginsLoaded(opts)).toBe(pending);
    await vi.advanceTimersByTimeAsync(PLUGIN_TIMEOUT_MS);
    expect(await pending).toHaveLength(1);
    expect(query).toHaveBeenCalledTimes(1);
    expect(isRegistryFrozen()).toBe(true);
  });
});
