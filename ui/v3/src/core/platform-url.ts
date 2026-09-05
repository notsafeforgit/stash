/** The server's base element is the public mount point, including proxy prefixes. */
export function applicationBaseURL(
  origin = window.location.origin,
  base = document.querySelector("base")?.getAttribute("href") ?? "/",
): URL {
  const url = new URL(base, origin);
  url.pathname = `${url.pathname.replace(/\/+$/, "")}/`;
  url.search = "";
  url.hash = "";
  return url;
}

export function getApplicationBasePath(): string {
  return applicationBaseURL().pathname;
}

/** Public href for raw history writes and anchors; preserves encoded filters. */
export function applicationHref(
  path: string,
  base = applicationBaseURL(),
): string {
  const url = joinPlatformURL(base, path);
  return url.pathname + url.search + url.hash;
}

export function joinPlatformURL(base: URL | string, path = ""): URL {
  const url = new URL(base);
  url.pathname = `${url.pathname.replace(/\/+$/, "")}/`;
  return new URL(path.replace(/^\/+/, ""), url);
}

export function getPlatformURL(path = ""): URL {
  let base = applicationBaseURL();
  if (import.meta.env.DEV) {
    if (import.meta.env.VITE_APP_PLATFORM_URL) {
      base = new URL(import.meta.env.VITE_APP_PLATFORM_URL);
    } else {
      base.port = import.meta.env.VITE_APP_PLATFORM_PORT ?? "8010";
      base.hostname = "127.0.0.1";
    }
  }
  return joinPlatformURL(base, path);
}

/** Convert a browser URL to the internal route path TanStack expects. */
export function applicationPath(
  href: string,
  base = applicationBaseURL(),
): string {
  const url = new URL(href, base);
  const prefix = base.pathname.replace(/\/+$/, "");
  const pathname =
    url.pathname === prefix
      ? "/"
      : url.pathname.startsWith(`${prefix}/`)
        ? url.pathname.slice(prefix.length)
        : url.pathname;
  return pathname + url.search + url.hash;
}
