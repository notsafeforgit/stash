import type { ServerOptions } from "vite";

/** Preserve browser sessions and origin checks across every development transport. */
export function developmentProxy(
  backend: string,
): NonNullable<ServerOptions["proxy"]> {
  return {
    // Native administration requires same-origin session requests. Preserve
    // the browser Host/Origin pair through this development proxy.
    "/api/v3/": { target: backend, changeOrigin: false },
    "/graphql": { target: backend, ws: true, changeOrigin: false },
    "/share": backend,
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
    "^/login(?:\\?|$|/)": {
      target: backend,
      changeOrigin: false,
      bypass(request) {
        const url = new URL(
          request.url ?? "/login",
          "http://development.invalid",
        );
        if (request.method === "GET" && url.pathname === "/login") {
          return `/login.html${url.search}`;
        }
      },
    },
    "/logout": backend,
  };
}
