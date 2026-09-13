import { withTimeout } from "@/utils/with-timeout";
import { applicationBaseURL } from "@/core/platform-url";
import type {} from "./background-fetch-types";

let registration: Promise<ServiceWorkerRegistration | undefined> | undefined;
export function registerOfflineWorker() {
  if (
    !import.meta.env.PROD ||
    !window.isSecureContext ||
    !("serviceWorker" in navigator)
  )
    return Promise.resolve(undefined);
  registration ??= navigator.serviceWorker
    .register(new URL("service-worker.js", applicationBaseURL()), {
      scope: applicationBaseURL().pathname,
      updateViaCache: "none",
    })
    .then(async (registered) => {
      // Await installation for a first download. Existing active workers remain
      // in charge until all tabs close; updates never interrupt forms or playback.
      if (!registered.active)
        await withTimeout(
          navigator.serviceWorker.ready,
          8000,
          "Offline installation",
        );
      return registered;
    })
    .catch(() => {
      registration = undefined;
      return undefined;
    });
  return registration;
}

export async function getDownloadRegistration() {
  if (!("serviceWorker" in navigator)) return undefined;
  return navigator.serviceWorker
    .getRegistration(applicationBaseURL().href)
    .catch(() => undefined);
}
