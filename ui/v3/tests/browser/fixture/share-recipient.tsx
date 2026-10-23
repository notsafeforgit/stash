import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { ThemeProvider } from "@/components/theme-provider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/sonner";
import { ShareViewer } from "@/components/sharing/share-viewer";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
import "./style.css";

history.replaceState(
  null,
  "",
  `/share/abcdefghijklmnopqrstuv/${location.search}`,
);
const root = document.getElementById("root");
if (!root) throw new Error("Missing fixture root");
createRoot(root).render(
  <IntlProvider locale="en-GB" messages={flattenMessages(messages)}>
    <ThemeProvider>
      <TooltipProvider>
        <ShareViewer />
        <Toaster />
      </TooltipProvider>
    </ThemeProvider>
  </IntlProvider>,
);
