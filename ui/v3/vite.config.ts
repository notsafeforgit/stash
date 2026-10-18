import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import viteCompression from "vite-plugin-compression";
import tailwindcss from "@tailwindcss/vite";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";
import { VitePWA } from "vite-plugin-pwa";
import { developmentProxy } from "./scripts/development-proxy.ts";
import { collectOfflineAssets } from "./scripts/offline-assets.ts";

const sourcemap = process.env.VITE_APP_SOURCEMAPS === "true";
const offlineAssets = new Set<string>();

export default defineConfig({
  base: "",
  build: {
    outDir: "build",
    sourcemap,
    reportCompressedSize: false,
    rollupOptions: {
      input: {
        app: "index.html",
        offline: "offline.html",
        share: "share.html",
      },
    },
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
    proxy: developmentProxy(
      process.env.VITE_APP_PLATFORM_URL ??
        `http://127.0.0.1:${process.env.VITE_APP_PLATFORM_PORT ?? "8010"}`,
    ),
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
    collectOfflineAssets(offlineAssets),
    VitePWA({
      strategies: "injectManifest",
      srcDir: "src/pwa",
      filename: "service-worker.ts",
      injectRegister: false,
      manifest: false,
      injectManifest: {
        globPatterns: ["**/*.{js,css,woff2,html,png}"],
        manifestTransforms: [
          async (entries) => ({
            manifest: entries.filter((entry) => offlineAssets.has(entry.url)),
            warnings: [],
          }),
        ],
      },
    }),
    viteCompression({
      algorithm: "gzip",
      // Workbox reads the original files to revision the offline bundle.
      deleteOriginFile: false,
      threshold: 0,
      filter: /\.(js|json|css|svg|md)$/i,
    }),
  ],
});
