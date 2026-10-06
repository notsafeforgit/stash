import { MockedProvider } from "@apollo/client/testing/react";
import { NativeSourceReview } from "@/components/detail/native-source-review";
import { MediaDetailLayout } from "@/components/detail/media-detail-layout";
import { createSourceReviewAPI } from "@/core/native-archive/source-review-api";
import {
  createSourceReviewOutbox,
  type SavedSourceReview,
} from "@/core/native-archive/source-review-outbox";
import { sourcePost } from "../../fixtures/source-review";

export function SourceReviewFixture() {
  const kind = new URLSearchParams(location.search).has("image")
    ? "image"
    : "scene";
  return (
    <MockedProvider>
      <div className="flex h-dvh flex-col overflow-hidden">
        <MediaDetailLayout
          title="Library item"
          mobilePageScroll
          onBack={() => {}}
          primaryContent={
            <div className="flex h-40 items-center justify-center">
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
              id: "source-review",
              label: "Sources",
              content: (
                <NativeSourceReview key={kind} kind={kind} localId="7" />
              ),
            },
          ]}
        />
      </div>
    </MockedProvider>
  );
}

// Test-only access to the real durable store, with no HTTP mutations.
declare global {
  interface Window {
    sourceReviewStorage: {
      prepare(prefix: string): Promise<SavedSourceReview>;
      read(prefix: string): Promise<SavedSourceReview | null>;
    };
  }
}
const storageTarget = { kind: "scene" as const, localId: "7" };
function journal(prefix: string) {
  return createSourceReviewOutbox(
    createSourceReviewAPI(
      new URL(`${prefix}api/v3/archive/`, location.origin).href,
    ),
  );
}
window.sourceReviewStorage = {
  prepare: (prefix) =>
    journal(prefix).prepare(
      storageTarget,
      sourcePost().association,
      "linked",
      "Reviewed source",
    ),
  read: (prefix) => journal(prefix).read(storageTarget),
};
