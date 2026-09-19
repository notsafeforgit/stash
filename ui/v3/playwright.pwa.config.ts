import { defineConfig } from "@playwright/test";

const host = process.env.PWA_TEST_HOST ?? "127.0.0.1";
const origin = `http://${host}:3034`;
const websocket = process.env.PLAYWRIGHT_WS_ENDPOINT;
const browserName = process.env.PWA_TEST_BROWSER ?? "chromium";
if (browserName !== "chromium" && browserName !== "webkit")
  throw new Error("PWA_TEST_BROWSER must be chromium or webkit");
// Remote Browserless needs a routable host and a secure-context exception for
// this synthetic HTTP fixture only. Production still requires HTTPS.
const launch = encodeURIComponent(
  JSON.stringify({
    args: [`--unsafely-treat-insecure-origin-as-secure=${origin}`],
  }),
);
export default defineConfig({
  testDir: "./tests/browser",
  testMatch: "pwa.browser.ts",
  workers: 1,
  reporter: "list",
  use: {
    baseURL: origin,
    trace: "retain-on-failure",
    // Background Fetch needs Chromium's full browser download service; the
    // minimal headless shell leaves browser-managed downloads unfinished.
    ...(browserName === "chromium" && !websocket && { channel: "chromium" }),
    ...(websocket &&
      browserName === "chromium" && {
        connectOptions: { wsEndpoint: `${websocket}?launch=${launch}` },
      }),
  },
  projects: [{ name: browserName, use: { browserName } }],
  webServer: {
    command: "node tests/browser/pwa-server.mjs",
    url: `${origin}/offline.html`,
    reuseExistingServer: false,
  },
});
