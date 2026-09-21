import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/browser",
  testMatch: "**/*.browser.ts",
  testIgnore: "pwa.browser.ts",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  workers: 2,
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: "http://127.0.0.1:3025",
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    { name: "chromium", use: { browserName: "chromium" } },
    {
      name: "webkit",
      use: { browserName: "webkit" },
      // Linux WebKit can hang while loading media after many contexts. A
      // single retry starts a fresh worker; timing assertions stay unchanged
      // and a repeated failure still fails CI.
      retries: process.env.CI ? 1 : 0,
    },
  ],
  webServer: {
    command:
      "node node_modules/vite/bin/vite.js --config tests/browser/vite.config.ts",
    url: "http://127.0.0.1:3025",
    reuseExistingServer: false,
  },
});
