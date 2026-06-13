import { createRoot } from "react-dom/client";
import { App } from "@/app";
import { installPagePinchZoomGuard } from "src/lib/prevent-page-pinch-zoom";
import { installVitePreloadErrorHandler } from "src/lib/vite-preload-error-handler";
import "@/styles/globals.css";

installVitePreloadErrorHandler();
installPagePinchZoomGuard();

const root = document.getElementById("root")!;
createRoot(root).render(<App />);
