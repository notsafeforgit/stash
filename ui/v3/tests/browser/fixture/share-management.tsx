import { createRoot } from "react-dom/client";
import { ApolloClient, HttpLink, InMemoryCache } from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { IntlProvider } from "react-intl";
import { ThemeProvider } from "@/components/theme-provider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/sonner";
import { SharesSettings } from "@/components/sharing/shares-settings";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
import "./style.css";

const client = new ApolloClient({
  link: new HttpLink({ uri: "/graphql" }),
  cache: new InMemoryCache(),
});
const root = document.getElementById("root");
if (!root) throw new Error("Missing fixture root");
createRoot(root).render(
  <ApolloProvider client={client}>
    <IntlProvider locale="en-GB" messages={flattenMessages(messages)}>
      <ThemeProvider>
        <TooltipProvider>
          <SharesSettings />
          <Toaster />
        </TooltipProvider>
      </ThemeProvider>
    </IntlProvider>
  </ApolloProvider>,
);
