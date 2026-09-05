import type { ApolloClient } from "@apollo/client";
import { createIntl } from "react-intl";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { StashPluginHost } from "./host";

vi.mock("./ui-exports", () => ({}));

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.resetModules();
});

describe("plugin startup", () => {
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
