import { fileURLToPath } from "node:url";
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
      "src/components/layout/mobile-nav-sheet": stubs,
      "@": source,
      src: source,
    },
  },
  server: { host: "127.0.0.1", port: 3025, strictPort: true },
  plugins: [react(), tailwindcss()],
});
