import { useMemo } from "react";
import { ApolloClient, ApolloLink, Observable } from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { EntityListPage } from "@/components/list/entity-list-page";
import { usePerformerListConfig } from "@/components/list/entity-list-configs";
import { createCache } from "@/core/create-client";
import { installVitePreloadErrorHandler } from "@/lib/vite-preload-error-handler";
import { performers } from "./merge-dialogs";

declare global {
  interface Window {
    performerListRequests: string[];
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
function List() {
  const config = usePerformerListConfig(() => {});
  return <EntityListPage config={config} />;
}

export function PerformerListFixture() {
  const client = useMemo(() => {
    window.performerListRequests = [];
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
                    count: items.length,
                    performers: items,
                  },
                };
                break;
              case "FindSavedFilters":
                data = { findSavedFilters: [] };
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
  return (
    <ApolloProvider client={client}>
      <div data-app-viewport className="flex h-dvh flex-col overflow-hidden">
        <List />
      </div>
    </ApolloProvider>
  );
}
