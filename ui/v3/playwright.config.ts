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
      use: {
        browserName: "webkit",
        // Linux WebKit composites through Mesa's llvmpipe. On Intel Xeon CPUs
        // (6975P-C, Platinum 8573C) the pinned image's Mesa 26.0.8 / LLVM 21
        // JIT faults in an AVX2 gather while drawing video, and the web process
        // dies with SIGSEGV. AMD runners are unaffected. Hiding AVX2 from
        // llvmpipe keeps its JIT; reassess when updating the Playwright image.
        launchOptions: {
          env: { ...process.env, GALLIUM_OVERRIDE_CPU_CAPS: "avx" },
        },
      },
      // Concurrent Linux media pipelines can stall each other on CI runners.
      // Isolate playback/frame-timing checks while retaining browser shards.
      workers: 1,
      // Linux WebKit can hang while loading media after many contexts. A
      // single retry starts a fresh worker; timing assertions stay unchanged
      // and a repeated failure still fails CI.
      retries: process.env.CI ? 1 : 0,
    },
  ],
  webServer: {
    command: process.env.STASH_BROWSER_DEV
      ? "node node_modules/vite/bin/vite.js --config tests/browser/vite.config.ts"
      : "node tests/browser/server.mjs",
    url: "http://127.0.0.1:3025",
    reuseExistingServer: false,
  },
});
