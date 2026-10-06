import { useState } from "react";
import { NativePerformerSources } from "@/components/detail/native-performer-sources";
import { CollectionDetailLayout } from "@/components/detail/collection-detail-layout";
import { DetailTabs } from "@/components/detail/detail-tabs";

export function PerformerSourcesFixture() {
  const [tab, setTab] = useState("scenes");
  return (
    <div className="flex h-dvh flex-col overflow-hidden">
      <CollectionDetailLayout title="Library performer" onBack={() => {}}>
        <div className="md:flex md:h-full md:flex-row">
          <aside className="p-4 md:w-72 md:shrink-0 md:border-r md:border-border">
            Library performer
          </aside>
          <div className="md:flex md:min-h-0 md:min-w-0 md:flex-1 md:flex-col">
            <DetailTabs
              tabs={[
                {
                  id: "scenes",
                  label: "Scenes",
                  content: <p>Library videos</p>,
                },
                {
                  id: "source-accounts",
                  label: "Source accounts",
                  content: <NativePerformerSources localId="7" />,
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
