import { MockedProvider } from "@apollo/client/testing/react";
import { useSearch, useNavigate } from "@tanstack/react-router";
import { Collections } from "@/components/archive/collections";
import { BottomTabBar } from "@/components/layout/bottom-tab-bar";
import { UserMenu } from "@/components/layout/user-menu";
import { createCollectionOutbox } from "@/core/native-archive/collection-outbox";
import { createCollectionAPI } from "@/core/native-archive/collection-api";
import { collectionInput, collectionID } from "../../fixtures/collections";
import {
  FindPerformersForSelectDocument,
  FindScenesForSelectDocument,
  FindImagesDocument,
} from "@/core/generated-graphql";
import { images } from "./image-lightbox";

const mocks = [
  {
    request: {
      query: FindPerformersForSelectDocument,
      variables: { filter: { q: "River", per_page: 25, page: 1 } },
    },
    maxUsageCount: Infinity,
    result: {
      data: {
        findPerformers: {
          __typename: "FindPerformersResultType",
          count: 1,
          performers: [
            {
              __typename: "Performer",
              id: "10",
              name: "River",
              disambiguation: "",
              aliases: [],
              image_path: "",
              birthdate: null,
              death_date: null,
            },
          ],
        },
      },
    },
  },
  {
    request: {
      query: FindScenesForSelectDocument,
      variables: { filter: { q: "Sample", per_page: 25, page: 1 } },
    },
    maxUsageCount: Infinity,
    result: {
      data: {
        findScenes: {
          __typename: "FindScenesResultType",
          count: 1,
          scenes: [
            {
              __typename: "Scene",
              id: "7",
              title: "Sample video",
              code: "",
              date: null,
              studio: null,
              files: [
                {
                  __typename: "VideoFile",
                  path: "/media/library/Purchased/River/sample.mp4",
                },
              ],
              paths: { __typename: "ScenePathsType", screenshot: null },
            },
          ],
        },
      },
    },
  },
  {
    request: {
      query: FindImagesDocument,
      variables: { filter: { q: "Sample", per_page: 25, page: 1 } },
    },
    maxUsageCount: Infinity,
    result: {
      data: {
        findImages: {
          __typename: "FindImagesResultType",
          count: 1,
          images: [{ ...images[0], id: "8", title: "Sample image" }],
        },
      },
    },
  },
];

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
    <MockedProvider mocks={mocks}>
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
