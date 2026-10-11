import {
  ApolloClient,
  ApolloLink,
  gql,
  InMemoryCache,
  Observable,
} from "@apollo/client";
import { ApolloProvider, useQuery } from "@apollo/client/react";
import { useState } from "react";
import { DeleteDialog } from "@/components/detail/delete-dialog";
import { useBulkMediaDelete } from "@/components/detail/use-bulk-media-delete";
import { Toaster } from "@/components/ui/sonner";

declare global {
  interface Window {
    bulkDeletionFixture: {
      batches: string[][];
      deleted: string[];
      reads: number;
      finishBatch: (success: boolean) => void;
    };
  }
}

const kind =
  new URLSearchParams(location.search).get("kind") === "image"
    ? "Image"
    : "Scene";
const list = kind === "Image" ? "findImages" : "findScenes";
const itemsField = kind === "Image" ? "images" : "scenes";
const mutationField = kind === "Image" ? "imagesDestroy" : "scenesDestroy";
const query = gql(
  `query DeletionFixtureItems { ${list} { count ${itemsField} { id title } } }`,
);
const ids = Array.from({ length: 120 }, (_, i) => String(i + 1));
window.bulkDeletionFixture = {
  batches: [],
  deleted: [],
  reads: 0,
  finishBatch: () => {},
};

function Items() {
  const { data } =
    useQuery<
      Record<string, { images?: { id: string }[]; scenes?: { id: string }[] }>
    >(query);
  const rows = data?.[list]?.[itemsField] ?? [];
  return (
    <div>
      {rows.map(({ id }) => (
        <div key={id} data-item-id={id}>
          Item {id}
          {id === "1" && <HostedDialog />}
        </div>
      ))}
    </div>
  );
}

function HostedDialog() {
  const [open, setOpen] = useState(true);
  const destroy = useBulkMediaDelete(kind);
  return (
    <DeleteDialog
      open={open}
      onOpenChange={setOpen}
      showFileOptions
      entityCountLabel="120 items"
      onConfirm={(options, progress) => destroy(ids, options, progress)}
    />
  );
}

export function BulkDeletionFixture() {
  const [client] = useState(() => {
    const state = window.bulkDeletionFixture;
    return new ApolloClient({
      cache: new InMemoryCache(),
      link: new ApolloLink(
        (operation) =>
          new Observable((observer) => {
            if (operation.operationName === "DeletionFixtureItems") {
              state.reads++;
              const remaining = ids.filter((id) => !state.deleted.includes(id));
              observer.next({
                data: {
                  [list]: {
                    __typename: `Find${kind}sResultType`,
                    count: remaining.length,
                    [itemsField]: remaining.map((id) => ({
                      __typename: kind,
                      id,
                      title: `Item ${id}`,
                    })),
                  },
                },
              });
              observer.complete();
              return;
            }
            const batch: string[] = operation.variables.ids;
            state.batches.push(batch);
            state.finishBatch = (success) => {
              if (success) {
                state.deleted.push(...batch);
                observer.next({ data: { [mutationField]: true } });
                observer.complete();
              } else observer.error(new Error("Connection lost"));
            };
          }),
      ),
    });
  });
  return (
    <ApolloProvider client={client}>
      <Items />
      <Toaster />
    </ApolloProvider>
  );
}
