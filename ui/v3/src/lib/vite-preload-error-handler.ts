type VitePreloadErrorEvent = Event & {
  payload?: unknown;
};

const reloadKeyPrefix = "stash:v3:vite-preload-reload:";
const retryWindowMs = 5 * 60 * 1000;

function payloadKey(payload: unknown) {
  if (payload instanceof Error) {
    return payload.message;
  }

  if (typeof payload === "string") {
    return payload;
  }

  if (
    payload &&
    typeof payload === "object" &&
    "target" in payload &&
    payload.target instanceof HTMLElement
  ) {
    const src =
      payload.target.getAttribute("href") ?? payload.target.getAttribute("src");
    if (src) return src;
  }

  return window.location.href;
}

function pruneOldReloadKeys(now: number) {
  try {
    for (let i = sessionStorage.length - 1; i >= 0; i--) {
      const key = sessionStorage.key(i);
      if (!key?.startsWith(reloadKeyPrefix)) continue;

      const lastAttempt = Number(sessionStorage.getItem(key) ?? "0");
      if (!Number.isFinite(lastAttempt) || now - lastAttempt > retryWindowMs) {
        sessionStorage.removeItem(key);
      }
    }
  } catch (_err) {
    // Storage can be unavailable in hardened browser modes. The handler can
    // still attempt a one-shot recovery for the current document.
  }
}

let attemptedWithoutStorage = false;

export function installVitePreloadErrorHandler() {
  if (typeof window === "undefined") return;

  pruneOldReloadKeys(Date.now());

  window.addEventListener("vite:preloadError", (event) => {
    const preloadEvent = event as VitePreloadErrorEvent;
    const now = Date.now();
    const key = `${reloadKeyPrefix}${payloadKey(preloadEvent.payload)}`;

    try {
      const lastAttempt = Number(sessionStorage.getItem(key) ?? "0");
      if (Number.isFinite(lastAttempt) && now - lastAttempt < retryWindowMs) {
        return;
      }

      sessionStorage.setItem(key, String(now));
    } catch (_err) {
      if (attemptedWithoutStorage) return;
      attemptedWithoutStorage = true;
    }

    event.preventDefault();
    window.location.reload();
  });
}
