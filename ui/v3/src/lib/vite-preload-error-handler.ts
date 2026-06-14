type VitePreloadErrorEvent = Event & {
  payload?: unknown;
};

const reloadKeyPrefix = "stash:v3:vite-preload-reload:";
const retryWindowMs = 5 * 60 * 1000;
const dynamicImportFailureMessages = [
  "Failed to fetch dynamically imported module",
  "error loading dynamically imported module",
  "Importing a module script failed",
];

function payloadMessage(payload: unknown): string | undefined {
  if (payload instanceof Error) {
    return payload.message;
  }

  if (typeof payload === "string") {
    return payload;
  }

  if (
    payload &&
    typeof payload === "object" &&
    "message" in payload &&
    typeof payload.message === "string"
  ) {
    return payload.message;
  }

  return undefined;
}

function payloadKey(payload: unknown) {
  const message = payloadMessage(payload);
  if (message) {
    return message;
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

function isDynamicImportFailure(payload: unknown) {
  const message = payloadMessage(payload);
  return dynamicImportFailureMessages.some((failureMessage) =>
    message?.includes(failureMessage),
  );
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
let attemptedForCurrentDocument = false;

function attemptReload(payload: unknown, event: Event) {
  if (attemptedForCurrentDocument) {
    event.preventDefault();
    return;
  }

  const key = `${reloadKeyPrefix}${payloadKey(payload)}`;

  try {
    const now = Date.now();
    const lastAttempt = Number(sessionStorage.getItem(key) ?? "0");
    if (Number.isFinite(lastAttempt) && now - lastAttempt < retryWindowMs) {
      return;
    }

    sessionStorage.setItem(key, String(now));
  } catch (_err) {
    if (attemptedWithoutStorage) return;
    attemptedWithoutStorage = true;
  }

  attemptedForCurrentDocument = true;
  event.preventDefault();
  window.location.reload();
}

export function installVitePreloadErrorHandler() {
  if (typeof window === "undefined") return;

  pruneOldReloadKeys(Date.now());

  window.addEventListener("vite:preloadError", (event) => {
    const preloadEvent = event as VitePreloadErrorEvent;
    attemptReload(preloadEvent.payload, event);
  });

  window.addEventListener("unhandledrejection", (event) => {
    if (!isDynamicImportFailure(event.reason)) return;
    attemptReload(event.reason, event);
  });

  window.addEventListener("error", (event) => {
    const payload = event.error ?? event.message;
    if (!isDynamicImportFailure(payload)) return;
    attemptReload(payload, event);
  });
}
