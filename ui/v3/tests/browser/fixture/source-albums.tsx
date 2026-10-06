import { useCallback, useState } from "react";
import { GallerySourceAlbums } from "@/components/archive/albums/gallery";
import { CollectionDetailLayout } from "@/components/detail/collection-detail-layout";
import { DetailTabs } from "@/components/detail/detail-tabs";

export function SourceAlbumsFixture() {
  const [tab, setTab] = useState("images");
  const [refreshes, setRefreshes] = useState(0);
  const published = useCallback(() => setRefreshes((value) => value + 1), []);
  return (
    <div
      className="flex h-dvh flex-col overflow-hidden"
      data-album-refresh-count={refreshes}
    >
      <CollectionDetailLayout title="Library gallery" onBack={() => {}}>
        <div className="md:flex md:h-full md:flex-row">
          <aside className="p-4 md:w-72 md:shrink-0 md:border-r md:border-border">
            Library gallery
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
                    <GallerySourceAlbums localId="12" onPublished={published} />
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
  );
}
