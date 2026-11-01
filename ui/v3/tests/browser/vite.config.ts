import { fileURLToPath } from "node:url";
import { readdirSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { defineConfig, type Connect, type Manifest } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const source = fileURLToPath(new URL("../../src", import.meta.url));
const fixture = fileURLToPath(new URL("./fixture", import.meta.url));
const output = fileURLToPath(new URL("../../dist/browser", import.meta.url));
const stubs = fileURLToPath(new URL("./fixture/stubs.tsx", import.meta.url));

// Exercise the production components and CSS without a backend or route codegen.
export default defineConfig({
  root: fixture,
  publicDir: false,
  resolve: {
    alias: {
      "src/hooks/default-filter": stubs,
      "@": source,
      src: source,
    },
  },
  server: { host: "127.0.0.1", port: 3025, strictPort: true },
  preview: { host: "127.0.0.1", port: 3025, strictPort: true },
  build: {
    outDir: output,
    emptyOutDir: true,
    manifest: true,
    minify: false,
    sourcemap: true,
    cssCodeSplit: false,
    rolldownOptions: {
      input: {
        // Include every isolated entry as well as the main SPA fixture.
        ...Object.fromEntries(
          readdirSync(fixture)
            .filter((name) => name.endsWith(".html"))
            .map((name) => [name.slice(0, -5), `${fixture}/${name}`]),
        ),
        "document-theme": `${fixture}/document-theme.tsx`,
        sharing: `${fixture}/sharing.tsx`,
      },
      output: {
        entryFileNames: "[name].js",
        // Distinguish a lazy component from shared fixture chunks with the
        // same basename, so delayed/failed-module tests intercept it exactly.
        chunkFileNames: (chunk) =>
          chunk.isDynamicEntry
            ? "assets/lazy-[name]-[hash].js"
            : "assets/[name]-[hash].js",
      },
    },
  },
  plugins: [
    react(),
    tailwindcss(),
    {
      name: "sharing-fixture",
      async configurePreviewServer(server) {
        const manifests: Record<string, Manifest> = Object.fromEntries(
          await Promise.all(
            ["document-theme", "sharing"].map(async (name) => [
              name,
              JSON.parse(
                await readFile(`${output}/.vite/${name}.json`, "utf8"),
              ),
            ]),
          ),
        );
        const asset = (manifest: Manifest, name: string) => {
          const entry = manifest[name];
          if (!entry) throw new Error(`Missing browser fixture asset: ${name}`);
          return `/${entry.file}`;
        };
        server.middlewares.use(
          fixturePages(async (url, html) => {
            const name = url === "/share.html" ? "sharing" : "document-theme";
            const manifest = manifests[name]!;
            return html
              .replace(`/${name}.tsx`, asset(manifest, `${name}.tsx`))
              .replace(
                `/@fs${source}/styles/globals.css`,
                asset(manifest, "style.css"),
              );
          }),
        );
      },
      configureServer(server) {
        server.middlewares.use(
          fixturePages((url, html) => server.transformIndexHtml(url, html)),
        );
      },
    },
  ],
});

function fixturePages(
  transformHTML: (url: string, html: string) => Promise<string>,
): Connect.NextHandleFunction {
  return async (request, response, next) => {
    if (/^\/image-(?:lightbox|files)(?:\?|$)/.test(request.url ?? "")) {
      // Match production's image policy, covered by the Go header test.
      // Copy image must decode its temporary blob URL with CSP enabled.
      response.setHeader("Content-Security-Policy", "img-src data: blob: *");
    }
    const themeShell = /^\/theme-shell\/(app|login|offline)$/.exec(
      request.url ?? "",
    );
    if (themeShell) {
      try {
        const entry = themeShell[1] === "app" ? "index" : themeShell[1];
        const html = await readFile(
          new URL(`../../${entry}.html`, import.meta.url),
          "utf8",
        );
        const transformed = await transformHTML(
          `/${entry}.html`,
          html
            .replace(/<base href="[^"]*"/, '<base href="/"')
            .replace("%COLOR%", "#202b33")
            .replace(
              /\/src\/(?:pwa\/offline-main|login-main|main)\.tsx/,
              "/document-theme.tsx",
            )
            .replace('<link rel="stylesheet" href="css" />', "")
            .replace('<script defer src="javascript"></script>', "")
            .replace(
              "</head>",
              `<link rel="stylesheet" href="/@fs${source}/styles/globals.css"></head>`,
            ),
        );
        response.setHeader("Content-Type", "text/html");
        response.end(transformed);
      } catch (error) {
        next(error);
      }
      return;
    }
    if (
      ["/manifest.json", "/favicon.ico", "/apple-touch-icon.png"].includes(
        request.url ?? "",
      )
    ) {
      response.statusCode = 204;
      response.end();
      return;
    }
    if (request.url === "/share/assets/share-theme.js") {
      try {
        response.setHeader("Content-Type", "text/javascript");
        response.end(
          await readFile(
            new URL("../../public/assets/share-theme.js", import.meta.url),
          ),
        );
      } catch (error) {
        next(error);
      }
      return;
    }
    if (
      /^\/share\/(?:[A-Za-z0-9_-]{22}|[A-Za-z0-9_-]{43})\/?$/.test(
        new URL(request.url ?? "/", "http://localhost").pathname,
      )
    ) {
      try {
        // Use the real document head too: Safari's theme and safe-area
        // behavior cannot be tested with a different fixture shell.
        const html = await readFile(
          new URL("../../share.html", import.meta.url),
          "utf8",
        );
        const transformed = await transformHTML(
          "/share.html",
          html
            .replace("/src/share-main.tsx", "/sharing.tsx")
            // Production extracts CSS into a head link, so it can paint
            // before the React module loads. Reproduce that ordering.
            .replace(
              "</head>",
              `<meta name="stash-share-id" content="abcdefghijklmnopqrstuv"><link rel="stylesheet" href="/@fs${source}/styles/globals.css"></head>`,
            ),
        );
        response.setHeader("Content-Type", "text/html");
        response.end(transformed);
      } catch (error) {
        next(error);
      }
      return;
    }
    next();
  };
}
