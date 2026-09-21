import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import {
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { IntlProvider } from "react-intl";
import { ShortcutProvider } from "@/components/shortcut-provider";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
import { SceneLightboxFixture } from "./scene-lightbox";
import "./style.css";

// The main browser fixture imports other players eagerly. Keep this entry
// isolated so the real lightbox chunk can be delayed independently of startup.
const rootRoute = createRootRoute();
const router = createRouter({
  routeTree: rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/lightbox-startup.html",
      component: SceneLightboxFixture,
    }),
  ]),
});
const root = document.getElementById("root");
if (!root) throw new Error("Missing fixture root");
createRoot(root).render(
  <StrictMode>
    <IntlProvider locale="en-GB" messages={flattenMessages(messages)}>
      <ShortcutProvider>
        <RouterProvider router={router} />
      </ShortcutProvider>
    </IntlProvider>
  </StrictMode>,
);
