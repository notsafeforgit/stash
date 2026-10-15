import {
  ApolloClient,
  ApolloLink,
  InMemoryCache,
  Observable,
} from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { useState } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { GalleryDeleteDialog } from "@/components/detail/gallery-delete-dialog";
import { Button } from "@/components/ui/button";
import { Toaster } from "@/components/ui/sonner";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
import "./style.css";

declare global {
  interface Window {
    galleryDeletionRequests: Record<string, unknown>[];
  }
}
window.galleryDeletionRequests = [];
const id = "00000000-0000-4000-8000-000000000001";
const client = new ApolloClient({
  cache: new InMemoryCache(),
  link: new ApolloLink(
    (operation) =>
      new Observable((observer) => {
        if (operation.operationName === "GalleryDeletePreview") {
          if (new URLSearchParams(location.search).has("fail")) {
            observer.error(new Error("Cannot load gallery membership"));
            return;
          }
          observer.next({
            data: {
              galleryDeletePreview: {
                __typename: "GalleryDeletePreview",
                gallery_count: 1,
                source_count: 1,
                image_count: 3,
                scene_count: 2,
                shared_image_count: 1,
                shared_scene_count: 1,
                zip_count: 0,
              },
            },
          });
        } else if (operation.operationName === "GalleryDestroy") {
          window.galleryDeletionRequests.push(operation.variables);
          observer.next({ data: { galleryDestroy: true } });
        } else {
          observer.error(
            new Error(`Unexpected operation: ${operation.operationName}`),
          );
          return;
        }
        observer.complete();
      }),
  ),
});

function Fixture() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button onClick={() => setOpen(true)}>Delete gallery</Button>
      <GalleryDeleteDialog
        open={open}
        onOpenChange={setOpen}
        ids={[id]}
        entityName="Mixed source album"
      />
      <Toaster />
    </>
  );
}

createRoot(document.getElementById("root")!).render(
  <IntlProvider locale="en-GB" messages={flattenMessages(messages)}>
    <ApolloProvider client={client}>
      <Fixture />
    </ApolloProvider>
  </IntlProvider>,
);
