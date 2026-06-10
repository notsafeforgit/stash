import { createRoot } from "react-dom/client";
import { App } from "@/app";
import { installPagePinchZoomGuard } from "src/lib/prevent-page-pinch-zoom";
import "@/styles/globals.css";

installPagePinchZoomGuard();

const root = document.getElementById("root")!;
createRoot(root).render(<App />);
