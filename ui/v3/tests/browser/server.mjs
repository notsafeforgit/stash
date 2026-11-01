// Bundle once per run instead of routing hundreds of development modules for
// every fresh browser context. Keep React's development checks and StrictMode.
process.env.NODE_ENV = "development";
const { cp } = await import("node:fs/promises");
const { build, loadConfigFromFile, preview } = await import("vite");
const configFile = "tests/browser/vite.config.ts";
const mode = "development";
const loaded = await loadConfigFromFile({ command: "build", mode }, configFile);
if (!loaded) throw new Error("Missing browser fixture configuration");
const config = loaded.config;
const inputs = config.build.rolldownOptions.input;
for (const [index, [name, entry]] of Object.entries(inputs).entries()) {
  // Separate entry graphs matter: an eager player in the main fixture must
  // not collapse the isolated startup fixture's dynamic import into it. Each
  // page also keeps its own CSS cascade instead of merging Tailwind roots.
  await build({
    ...config,
    configFile: false,
    mode,
    build: {
      ...config.build,
      emptyOutDir: index === 0,
      manifest: `.vite/${name}.json`,
      rolldownOptions: {
        ...config.build.rolldownOptions,
        input: { [name]: entry },
      },
    },
  });
}
// Vite preview serves synthetic media with byte-range support.
await Promise.all(
  ["media", "player.mp4"].map((name) =>
    cp(
      new URL(`./fixture/${name}`, import.meta.url),
      `${config.build.outDir}/${name}`,
      {
        recursive: true,
      },
    ),
  ),
);
const server = await preview({ configFile, mode });
server.printUrls();
