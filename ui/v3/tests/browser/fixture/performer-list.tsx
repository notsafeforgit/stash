import { useMemo } from "react";
import { ApolloClient, ApolloLink, Observable } from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { Route as performerRoute } from "@/routes/performers/index";
import { createCache } from "@/core/create-client";
import type { PerformerCreateInput } from "@/core/generated-graphql";
import { installVitePreloadErrorHandler } from "@/lib/vite-preload-error-handler";
import { performers } from "./merge-dialogs";
import { playerConfiguration } from "./player-configuration";

declare global {
  interface Window {
    performerListRequests: string[];
    createdPerformerInputs: PerformerCreateInput[];
  }
}

// Match production's error recovery, including during a lazy overlay import.
installVitePreloadErrorHandler();
const items = Array.from({ length: 40 }, (_, index) => ({
  ...performers[0]!,
  id: `00000000-0000-4000-8000-${String(index + 1).padStart(12, "0")}`,
  name: `Performer ${index + 1}`,
  aliases: [],
  urls: [],
}));
export function PerformerListFixture() {
  const client = useMemo(() => {
    window.performerListRequests = [];
    window.createdPerformerInputs = [];
    const listItems = new URLSearchParams(location.search).has("empty")
      ? []
      : items;
    return new ApolloClient({
      cache: createCache(),
      link: new ApolloLink(
        (operation) =>
          new Observable((observer) => {
            const name = operation.operationName;
            window.performerListRequests.push(name ?? "");
            let data: object;
            switch (name) {
              case "FindPerformers":
                data = {
                  findPerformers: {
                    __typename: "FindPerformersResultType",
                    count: listItems.length,
                    performers: listItems,
                  },
                };
                break;
              case "FindSavedFilters":
                data = { findSavedFilters: [] };
                break;
              case "ListPerformerScrapers":
                data = { listScrapers: [] };
                break;
              case "Configuration":
                data = { configuration: playerConfiguration };
                break;
              case "PerformerCreate":
                window.createdPerformerInputs.push(operation.variables.input);
                data = {
                  performerCreate: {
                    ...items[0],
                    id: "00000000-0000-4000-8000-000000000100",
                    name: operation.variables.input.name,
                  },
                };
                break;
              case "BulkCustomFieldSummary":
                data = {
                  bulkCustomFieldSummary: {
                    __typename: "BulkCustomFieldSummary",
                    count: items.length,
                    shared_names: [],
                    partial_names: [],
                  },
                };
                break;
              default:
                throw new Error(`Unexpected query ${name}`);
            }
            observer.next({ data });
            observer.complete();
          }),
      ),
    });
  }, []);
  const PerformersPage = performerRoute.options.component;
  if (!PerformersPage) throw new Error("Missing performer route component");
  return (
    <ApolloProvider client={client}>
      <div data-app-viewport className="flex h-dvh flex-col overflow-hidden">
        <PerformersPage />
      </div>
    </ApolloProvider>
  );
}
