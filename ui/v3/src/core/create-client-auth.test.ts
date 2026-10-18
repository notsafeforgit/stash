// @vitest-environment jsdom
import { gql } from "@apollo/client";
import { afterEach, expect, it, vi } from "vitest";
import { createClient } from "./create-client";
import { getPlatformURL } from "./platform-url";

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  document.head.innerHTML = "";
});

it.each(["/", "/stash/"])(
  "uses the browser session and login under %s even with a development backend override",
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
      .mockResolvedValue(new Response("Unauthorized", { status: 401 }));
    vi.stubGlobal("fetch", fetch);

    expect(getPlatformURL("graphql").href).toBe(
      `https://dev.example.test${prefix}graphql`,
    );
    const { client, wsClient } = createClient();
    try {
      await expect(
        client.query({
          query: gql`query AuthenticatedDevelopment { systemStatus { status } }`,
          fetchPolicy: "network-only",
        }),
      ).rejects.toThrow();
      expect(fetch).toHaveBeenCalledWith(
        `https://dev.example.test${prefix}graphql`,
        expect.objectContaining({ credentials: "same-origin" }),
      );
      const login = new URL(location.href);
      expect(login.origin + login.pathname).toBe(
        `https://dev.example.test${prefix}login`,
      );
      expect(login.searchParams.get("returnURL")).toBe(currentPage);
      expect(alert).not.toHaveBeenCalled();
    } finally {
      client.stop();
      await wsClient.dispose();
    }
  },
);
