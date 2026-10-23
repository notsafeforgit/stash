import { createRoot } from "react-dom/client";
import { ApolloClient, HttpLink, InMemoryCache } from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { IntlProvider } from "react-intl";
import { ThemeProvider } from "@/components/theme-provider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/sonner";
import { SharesSettings } from "@/components/sharing/shares-settings";
import { useShareAction } from "@/components/sharing/share-action";
import { Button } from "@/components/ui/button";
import * as GQL from "@/core/generated-graphql";
import { libraryID } from "../../fixtures/library";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
import "./style.css";

const client = new ApolloClient({
  link: new HttpLink({ uri: "/graphql" }),
  cache: new InMemoryCache(),
});
const root = document.getElementById("root");
if (!root) throw new Error("Missing fixture root");
function ShareFixtureActions() {
  const size = Number(
    new URLSearchParams(location.search).get("selection") ?? 2,
  );
  const selection = useShareAction(
    Array.from({ length: size }, (_, index) => ({
      kind: index === 1 ? GQL.ShareEntityKind.Image : GQL.ShareEntityKind.Scene,
      id: libraryID(index + 1),
      name: `Title ${index + 1} ${"UnbrokenTitle".repeat(30)}`,
    })),
  );
  const performer = useShareAction([], undefined, {
    kind: GQL.ShareLibraryKind.Performer,
    id: libraryID(999),
    name: "Example performer",
  });
  return (
    <>
      <Button onClick={selection.open}>Share selection</Button>
      {selection.dialog}
      <Button onClick={performer.open}>Share performer</Button>
      {performer.dialog}
    </>
  );
}
createRoot(root).render(
  <ApolloProvider client={client}>
    <IntlProvider locale="en-GB" messages={flattenMessages(messages)}>
      <ThemeProvider>
        <TooltipProvider>
          <div className="h-dvh overflow-y-auto">
            <ShareFixtureActions />
            <SharesSettings />
          </div>
          <Toaster />
        </TooltipProvider>
      </ThemeProvider>
    </IntlProvider>
  </ApolloProvider>,
);
