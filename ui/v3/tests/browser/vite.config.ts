import { fileURLToPath } from "node:url";
import { readFile } from "node:fs/promises";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const source = fileURLToPath(new URL("../../src", import.meta.url));
const fixture = fileURLToPath(new URL("./fixture", import.meta.url));
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
  plugins: [
    react(),
    tailwindcss(),
    {
      name: "sharing-fixture",
      configureServer(server) {
        server.middlewares.use(async (request, response, next) => {
          if (request.url === "/share/assets/share-theme.js") {
            try {
              response.setHeader("Content-Type", "text/javascript");
              response.end(
                await readFile(
                  new URL(
                    "../../public/assets/share-theme.js",
                    import.meta.url,
                  ),
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
              const transformed = await server.transformIndexHtml(
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
        });
      },
    },
  ],
});
