import { MockedProvider } from "@apollo/client/testing/react";
import { NativeMetadataReview } from "@/components/detail/native-metadata-review";
import { MediaDetailLayout } from "@/components/detail/media-detail-layout";
import { ids } from "../../fixtures/metadata-review";

export function MetadataReviewFixture() {
  const kind = new URLSearchParams(location.search).has("image")
    ? "image"
    : "scene";
  return (
    <MockedProvider mocks={[]}>
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
                    id: ids.entity,
                    performers: [{ id: ids.performer, name: "River" }],
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
