// Bundle once per run instead of routing hundreds of development modules for
// every fresh browser context. Keep React's development checks and StrictMode.
process.env.NODE_ENV = "development";
const { build, preview } = await import("vite");
const config = {
  configFile: "tests/browser/vite.config.ts",
  mode: "development",
};
await build(config);
const server = await preview(config);
server.printUrls();
