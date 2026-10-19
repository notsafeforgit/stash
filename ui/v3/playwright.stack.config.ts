import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/stack",
  testMatch: "**/*.browser.ts",
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  timeout: 90000,
  reporter: [
    ["list"],
    ["html", { outputFolder: "stack-report", open: "never" }],
  ],
  use: {
    baseURL: "http://127.0.0.1:3036",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    { name: "chromium", use: { browserName: "chromium" } },
    { name: "webkit", use: { browserName: "webkit" } },
  ],
  webServer: {
    command: "cd ../.. && dist/stash-browser-stack",
    url: "http://127.0.0.1:3036/mounted/healthz",
    reuseExistingServer: false,
    timeout: 60000,
  },
});
