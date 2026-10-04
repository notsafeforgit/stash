import { useState } from "react";
import {
  ApolloClient,
  ApolloLink,
  InMemoryCache,
  Observable,
} from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { TagBulkEditSheet } from "@/components/detail/tag-bulk-edit-sheet";
import { Toaster } from "@/components/ui/sonner";

export function BulkUpdateFixture() {
  const [open, setOpen] = useState(true);
  const [saved, setSaved] = useState(0);
  const [input, setInput] = useState<unknown>(null);
  const [client] = useState(
    () =>
      new ApolloClient({
        cache: new InMemoryCache(),
        link: new ApolloLink(
          (operation) =>
            new Observable((observer) => {
              if (operation.operationName === "BulkCustomFieldSummary") {
                observer.next({
                  data: {
                    bulkCustomFieldSummary: {
                      __typename: "BulkCustomFieldSummary",
                      count: 2,
                      shared_names: [],
                      partial_names: [],
                    },
                  },
                });
              } else if (operation.operationName === "BulkTagUpdate") {
                setInput(operation.variables.input);
                const kind = new URLSearchParams(location.search).get("result");
                if (kind === "error") {
                  observer.error(new Error("Update rejected"));
                  return;
                }
                observer.next({
                  data: {
                    bulkTagUpdate: {
                      __typename: "BulkUpdateResult",
                      status: kind === "queued" ? "QUEUED" : "COMPLETED",
                      job_id: kind === "queued" ? "1" : null,
                      selected_count: kind === "queued" ? 30 : 2,
                      updated_ids:
                        kind === "queued" || kind === "invalid"
                          ? []
                          : ["1", "2"],
                    },
                  },
                });
              } else if (operation.operationName === "FindJob") {
                observer.next({
                  data: {
                    findJob: {
                      __typename: "Job",
                      id: "1",
                      status: "FINISHED",
                      description: "Bulk edit",
                      progress: 1,
                      subTasks: [],
                      error: null,
                      addTime: "2026-10-04T00:00:00Z",
                      startTime: null,
                      endTime: null,
                    },
                  },
                });
              }
              observer.complete();
            }),
        ),
      }),
  );
  return (
    <ApolloProvider client={client}>
      <Toaster />
      <div data-testid="saved">{saved}</div>
      <pre data-testid="input">{JSON.stringify(input)}</pre>
      <TagBulkEditSheet
        open={open}
        onOpenChange={setOpen}
        onSaved={() => setSaved((count) => count + 1)}
        items={[
          { id: "1", parents: [], children: [] },
          { id: "2", parents: [], children: [] },
        ]}
        totalCount={30}
        applyToAllTarget={{ findFilter: { q: "example" } }}
      />
    </ApolloProvider>
  );
}
