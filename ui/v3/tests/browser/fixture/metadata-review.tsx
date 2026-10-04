import { FindPerformersForSelectDocument } from "@/core/generated-graphql";
import { MockedProvider } from "@apollo/client/testing/react";
import { NativeMetadataReview } from "@/components/detail/native-metadata-review";
import { MediaDetailLayout } from "@/components/detail/media-detail-layout";

export function MetadataReviewFixture() {
  const kind = new URLSearchParams(location.search).has("image")
    ? "image"
    : "scene";
  return (
    <MockedProvider
      mocks={[
        {
          request: {
            query: FindPerformersForSelectDocument,
            variables: { filter: { q: "Cloud", per_page: 25, page: 1 } },
          },
          result: {
            data: {
              findPerformers: {
                __typename: "FindPerformersResultType",
                count: 1,
                performers: [
                  {
                    __typename: "Performer",
                    id: "12",
                    name: "Cloud",
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
      ]}
    >
      <div className="flex h-dvh flex-col overflow-hidden">
        <MediaDetailLayout
          title="Library item"
          mobilePageScroll
          onBack={() => {}}
          primaryContent={
            <div className="flex h-40 items-center justify-center text-white">
              Media viewer
            </div>
          }
          tabs={[
            {
              id: "details",
              label: "Details",
              content: <p>Library metadata</p>,
            },
            {
              id: "metadata-review",
              label: "Metadata review",
              content: (
                <NativeMetadataReview
                  key={kind}
                  kind={kind}
                  entity={{
                    id: "7",
                    performers: [{ id: "10", name: "River" }],
                    tags: [],
                  }}
                />
              ),
            },
          ]}
        />
      </div>
    </MockedProvider>
  );
}

// Test-only bridge to exercise the real IndexedDB journal across tabs and
// deployment prefixes without exposing application data or issuing HTTP writes.
import { createMetadataReviewAPI } from "@/core/native-archive/metadata-review-api";
import {
  createMetadataReviewOutbox,
  type SavedReview,
} from "@/core/native-archive/metadata-review-outbox";
import { preview } from "../../fixtures/metadata-review";

declare global {
  interface Window {
    metadataReviewStorage: {
      prepare(prefix: string): Promise<SavedReview>;
      read(prefix: string): Promise<SavedReview | null>;
    };
  }
}
const storageTarget = { kind: "scene" as const, localId: "7" };
function journal(prefix: string) {
  return createMetadataReviewOutbox(
    createMetadataReviewAPI(
      new URL(`${prefix}api/v3/archive/`, location.origin).href,
    ),
  );
}
window.metadataReviewStorage = {
  prepare: (prefix) => journal(prefix).prepare(storageTarget, preview()),
  read: (prefix) => journal(prefix).read(storageTarget),
};
