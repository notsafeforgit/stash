import { useCallback, useState } from "react";
import { GallerySourceAlbums } from "@/components/archive/albums/gallery";
import { GalleryOrigin } from "@/components/archive/albums/gallery-origin";
import { useGallerySources } from "@/components/archive/albums/gallery-sources";
import { CollectionDetailLayout } from "@/components/detail/collection-detail-layout";
import { DetailTabs } from "@/components/detail/detail-tabs";
import { AssociationFixtureProvider } from "./association-provider";

export function SourceAlbumsFixture() {
  const [tab, setTab] = useState("images");
  const [refreshes, setRefreshes] = useState(0);
  const sources = useGallerySources("12");
  const params = new URLSearchParams(window.location.search);
  const gallery = {
    folder: params.get("folder") ? { path: params.get("folder") ?? "" } : null,
    files: params.get("archive") ? [{ path: params.get("archive") ?? "" }] : [],
  };
  const published = useCallback(() => setRefreshes((value) => value + 1), []);
  return (
    <AssociationFixtureProvider>
      <div
        className="flex h-dvh flex-col overflow-hidden"
        data-album-refresh-count={refreshes}
      >
        <CollectionDetailLayout title="Library gallery" onBack={() => {}}>
          <div className="md:flex md:h-full md:flex-row">
            <aside className="p-4 md:w-72 md:shrink-0 md:border-r md:border-border">
              <GalleryOrigin
                gallery={gallery}
                sources={sources}
                onViewAlbums={() => setTab("source-albums")}
              />
            </aside>
            <div className="md:flex md:min-h-0 md:min-w-0 md:flex-1 md:flex-col">
              <DetailTabs
                tabs={[
                  {
                    id: "images",
                    label: "Images",
                    content: <p>Library images</p>,
                  },
                  {
                    id: "source-albums",
                    label: "Source albums",
                    content: (
                      <GallerySourceAlbums
                        sources={sources}
                        onPublished={published}
                      />
                    ),
                  },
                ]}
                activeTab={tab}
                onTabChange={setTab}
              />
            </div>
          </div>
        </CollectionDetailLayout>
      </div>
    </AssociationFixtureProvider>
  );
}
