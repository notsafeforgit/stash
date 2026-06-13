import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import viteCompression from "vite-plugin-compression";
import tailwindcss from "@tailwindcss/vite";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";

const sourcemap = process.env.VITE_APP_SOURCEMAPS === "true";

export default defineConfig({
  base: "",
  build: {
    outDir: "build",
    sourcemap,
    reportCompressedSize: false,
  },
  resolve: {
    tsconfigPaths: true,
  },
  optimizeDeps: {
    entries: "src/main.tsx",
  },
  server: {
    host: "0.0.0.0",
    port: 3002,
    allowedHosts: ["devstash.ak.codes"],
    cors: false,
    proxy: (() => {
      // Forward Go backend routes to the stash server.
      // Use trailing slashes on path prefixes so e.g. "/scene/" doesn't
      // accidentally match the SPA route "/scenes/".
      const backend =
        process.env.VITE_APP_PLATFORM_URL ?? "http://127.0.0.1:8010";
      return {
        "/graphql": { target: backend, ws: true },
        "/scene/": backend,
        "/image/": backend,
        "/gallery/": backend,
        "/performer/": backend,
        "/studio/": backend,
        "/group/": backend,
        "/tag/": backend,
        "/plugin/": backend,
        "/css": backend,
        "/javascript": backend,
        "/customlocales": backend,
        "/custom/": backend,
        "/login": backend,
        "/logout": backend,
      };
    })(),
  },
  publicDir: "public",
  plugins: [
    TanStackRouterVite({
      routesDirectory: "src/routes",
      generatedRouteTree: "src/routeTree.gen.ts",
      // Split each route's component into its own chunk so heavy pages
      // (package manager, identify dialog, duplicate checkers) load on
      // demand instead of shipping in the initial bundle.
      autoCodeSplitting: true,
    }),
    react(),
    tailwindcss(),
    viteCompression({
      algorithm: "gzip",
      deleteOriginFile: true,
      threshold: 0,
      filter: /\.(js|json|css|svg|md)$/i,
    }),
  ],
});
