/// <reference lib="webworker" />
import { precacheAndRoute, matchPrecache } from "workbox-precaching";
import { registerRoute } from "workbox-routing";
import { initializeWorkerOfflineScope } from "@/components/offline/offline-scope";
import { finishBackgroundDownload } from "./background-fetch-handler";
import type { BackgroundFetchRegistration } from "./background-fetch-types";

declare const self: ServiceWorkerGlobalScope & {
  __WB_MANIFEST: { url: string; revision: string | null }[];
};
interface BackgroundFetchEvent extends ExtendableEvent {
  readonly registration: BackgroundFetchRegistration;
}
declare global {
  interface ServiceWorkerGlobalScopeEventMap {
    backgroundfetchsuccess: BackgroundFetchEvent;
    backgroundfetchfail: BackgroundFetchEvent;
    backgroundfetchabort: BackgroundFetchEvent;
    backgroundfetchclick: BackgroundFetchEvent;
  }
}

initializeWorkerOfflineScope(self.registration.scope);
// Only bundled offline-view assets enter this cache. API responses, server
// configuration, remote artwork and streamed media are deliberately excluded.
precacheAndRoute(self.__WB_MANIFEST, {
  cleanURLs: false,
  directoryIndex: undefined,
});

registerRoute(
  ({ request, url }) => {
    if (
      request.mode !== "navigate" ||
      !url.href.startsWith(self.registration.scope)
    )
      return false;
    const path = url.pathname.slice(
      new URL(self.registration.scope).pathname.length,
    );
    return /^(?:$|index\.html$|offline(?:\/|$)|scenes(?:\/|$)|performers(?:\/|$)|galleries(?:\/|$)|images(?:\/|$)|groups(?:\/|$)|studios(?:\/|$)|tags(?:\/|$)|settings(?:\/|$))/.test(
      path,
    );
  },
  async ({ request }) => {
    const abort = new AbortController();
    const timeout = setTimeout(() => abort.abort(), 5000);
    try {
      const response = await fetch(request, { signal: abort.signal });
      if (response.status < 500) return response;
      throw new Error("Server unavailable");
    } catch {
      const fallback = await matchPrecache("offline.html");
      if (!fallback) return Response.error();
      // The same cached document must work at / and at deeply nested routes,
      // including reverse-proxy prefixes. No server bootstrap data is cached.
      const base = self.registration.scope
        .replaceAll("&", "&amp;")
        .replaceAll('"', "&quot;");
      const html = (await fallback.text()).replace(
        /<base href="\.\/"\s*\/?>/,
        `<base href="${base}">`,
      );
      // Preserve the server's CSP and other security headers on cold launches.
      // The decoded, rewritten body no longer has the original byte metadata.
      const headers = new Headers(fallback.headers);
      headers.set("Content-Type", "text/html; charset=utf-8");
      for (const name of [
        "Content-Length",
        "Content-Encoding",
        "ETag",
        "Last-Modified",
      ])
        headers.delete(name);
      return new Response(html, { headers });
    } finally {
      clearTimeout(timeout);
    }
  },
);

self.addEventListener("activate", (event) => {
  // Claim the first visit so offline navigation works immediately. Subsequent
  // versions wait for open windows to close; no skipWaiting/reload surprise.
  event.waitUntil(self.clients.claim());
});
for (const name of [
  "backgroundfetchsuccess",
  "backgroundfetchfail",
  "backgroundfetchabort",
] as const) {
  self.addEventListener(name, (event) => {
    event.waitUntil(
      finishBackgroundDownload(event.registration, self.registration),
    );
  });
}
self.addEventListener("backgroundfetchclick", (event) => {
  event.waitUntil(
    self.clients.openWindow(new URL("offline", self.registration.scope).href),
  );
});
