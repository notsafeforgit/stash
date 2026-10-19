// @vitest-environment jsdom
import { gql } from "@apollo/client";
import { afterEach, expect, it, vi } from "vitest";
import { createClient } from "./create-client";
import { getPlatformURL } from "./platform-url";
import { sessionRecovery } from "./session-recovery";

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  document.head.innerHTML = "";
});

it.each(["/", "/stash/"])(
  "recovers the browser session in place under %s even with a development backend override",
  async (prefix) => {
    vi.stubEnv("DEV", true);
    vi.stubEnv("VITE_APP_PLATFORM_URL", "http://127.0.0.1:9999");
    vi.stubEnv("VITE_APP_PLATFORM_PORT", "9998");
    const currentPage = `https://dev.example.test${prefix}scenes?p=2`;
    const location = {
      href: currentPage,
      toString() {
        return this.href;
      },
    };
    vi.stubGlobal(
      "window",
      new Proxy(window, {
        get(target, property) {
          return property === "location"
            ? location
            : Reflect.get(target, property);
        },
      }),
    );
    document.head.innerHTML = `<base href="${prefix}">`;
    const alert = vi.fn();
    vi.stubGlobal("alert", alert);
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(new Response("Unauthorized", { status: 401 }))
      .mockResolvedValueOnce(
        Response.json({ data: { systemStatus: { status: "OK" } } }),
      );
    vi.stubGlobal("fetch", fetch);

    expect(getPlatformURL("graphql").href).toBe(
      `https://dev.example.test${prefix}graphql`,
    );
    const { client, wsClient } = createClient();
    const unsubscribe = sessionRecovery.subscribe(() => {});
    try {
      const query = client.query({
        query: gql`query AuthenticatedDevelopment { systemStatus { status } }`,
        fetchPolicy: "network-only",
      });
      await vi.waitFor(() => expect(sessionRecovery.getSnapshot()).toBe(true));
      expect(location.href).toBe(currentPage);
      sessionRecovery.authenticated();
      await expect(query).resolves.toMatchObject({
        data: { systemStatus: { status: "OK" } },
      });
      expect(fetch).toHaveBeenCalledTimes(2);
      expect(fetch.mock.calls[0]?.[0]).toBe(
        `https://dev.example.test${prefix}graphql`,
      );
      expect(fetch.mock.calls[0]?.[1]?.credentials).toBe("same-origin");
      expect(fetch.mock.calls[0]?.[1]?.body).toBe(
        fetch.mock.calls[1]?.[1]?.body,
      );
      expect(location.href).toBe(currentPage);
      expect(alert).not.toHaveBeenCalled();
    } finally {
      unsubscribe();
      client.stop();
      await wsClient.dispose();
    }
  },
);
