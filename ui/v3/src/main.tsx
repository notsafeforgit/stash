import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "@/app";
import { SceneLightbox } from "@/components/lightbox/deferred-lightboxes";
import { registerOfflineWorker } from "@/pwa/register";
import { installPagePinchZoomGuard } from "src/lib/prevent-page-pinch-zoom";
import { installVitePreloadErrorHandler } from "src/lib/vite-preload-error-handler";
import "@/styles/globals.css";

void registerOfflineWorker();
installVitePreloadErrorHandler();
installPagePinchZoomGuard();

const root = document.getElementById("root");
if (!root) throw new Error("Missing application root");
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);

// Scene playback is a primary entry path. Warm the lightbox and its shared
// player asynchronously on every launch; mounting/media loading waits for use.
// Opening the lightbox retries a failed preload and provides visible recovery.
void SceneLightbox.preload().catch(() => {});
