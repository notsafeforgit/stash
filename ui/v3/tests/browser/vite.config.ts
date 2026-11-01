import { fileURLToPath } from "node:url";
import { cp, readFile } from "node:fs/promises";
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
        index: `${fixture}/index.html`,
        "share-recipient": `${fixture}/share-recipient.html`,
        "document-theme": `${fixture}/document-theme.tsx`,
        sharing: `${fixture}/sharing.tsx`,
      },
      output: { entryFileNames: "[name].js" },
    },
  },
  plugins: [
    react(),
    tailwindcss(),
    {
      name: "sharing-fixture",
      async writeBundle() {
        // Synthetic media needs byte-range support from Vite's static server.
        await cp(`${fixture}/media`, `${output}/media`, { recursive: true });
      },
      async configurePreviewServer(server) {
        const manifest: Manifest = JSON.parse(
          await readFile(`${output}/.vite/manifest.json`, "utf8"),
        );
        const asset = (name: string) => {
          const entry = manifest[name];
          if (!entry) throw new Error(`Missing browser fixture asset: ${name}`);
          return `/${entry.file}`;
        };
        server.middlewares.use(
          fixturePages(async (_url, html) =>
            html
              .replace("/document-theme.tsx", asset("document-theme.tsx"))
              .replace("/sharing.tsx", asset("sharing.tsx"))
              .replace(`/@fs${source}/styles/globals.css`, asset("style.css")),
          ),
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
