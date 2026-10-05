import { MockedProvider } from "@apollo/client/testing/react";
import { useSearch, useNavigate } from "@tanstack/react-router";
import { Collections } from "@/components/archive/collections";
import { BottomTabBar } from "@/components/layout/bottom-tab-bar";
import { UserMenu } from "@/components/layout/user-menu";
import { createCollectionOutbox } from "@/core/native-archive/collection-outbox";
import { createCollectionAPI } from "@/core/native-archive/collection-api";
import { collectionInput, collectionID } from "../../fixtures/collections";

function storage(prefix: string) {
  return createCollectionOutbox(
    createCollectionAPI(
      new URL(`${prefix}api/v3/archive/`, location.origin).href,
    ),
  );
}
const collectionStorage = {
  read: (prefix: string) => storage(prefix).read(collectionID),
  prepare: (prefix: string, label: string) =>
    storage(prefix).prepare({ ...collectionInput(), label }),
};
declare global {
  interface Window {
    collectionStorage: typeof collectionStorage;
  }
}
window.collectionStorage = collectionStorage;

export function CollectionsFixture() {
  const { collection, create, ...filter } = useSearch({ from: "/collections" });
  const navigate = useNavigate({ from: "/collections" });
  return (
    <MockedProvider>
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="hidden justify-end p-2 md:flex">
          <UserMenu />
        </div>
        <Collections
          key={JSON.stringify(filter)}
          filter={filter}
          selected={collection}
          create={create}
          onFilterChange={(filter) => void navigate({ search: filter })}
          onSelect={(collection, create) =>
            void navigate({ search: { ...filter, collection, create } })
          }
        />
        <BottomTabBar />
      </div>
    </MockedProvider>
  );
}
