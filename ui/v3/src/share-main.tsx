import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { ThemeProvider } from "@/components/theme-provider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { ShareViewer } from "@/components/sharing/share-viewer";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
import { installPagePinchZoomGuard } from "@/lib/prevent-page-pinch-zoom";
import "@/styles/globals.css";

// Deliberately independent of App: no account configuration, plugins, Apollo
// session, service worker registration or offline library initialization.
installPagePinchZoomGuard();
const root = document.getElementById("root");
if (!root) throw new Error("Missing application root");
createRoot(root).render(
  <IntlProvider
    locale="en-GB"
    defaultLocale="en-GB"
    messages={flattenMessages(messages)}
  >
    <ThemeProvider>
      <TooltipProvider>
        <ShareViewer />
      </TooltipProvider>
    </ThemeProvider>
  </IntlProvider>,
);
