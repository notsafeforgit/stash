import { useState } from "react";
import { EntityCard } from "@/components/cards/entity-card";
import { CollectionDetailLayout } from "@/components/detail/collection-detail-layout";
import { DetailTabs } from "@/components/detail/detail-tabs";
import { EntityList } from "@/components/list/entity-list";
import { CardAspectContext } from "@/components/list/card-aspect-context";
import { VirtualizedItemList } from "@/components/list/virtualized-item-list";
import { useListSelect } from "@/components/list/use-list-select";
import { useFilterState } from "@/components/list/use-filter-state";
import { Button } from "@/components/ui/button";
import { FilterMode, PreviewImageDynamicRange } from "@/core/generated-graphql";
import { DisplayMode } from "@/models/list-filter/types";
import { useMediaQuery } from "@/utils/screen";

const items = Array.from({ length: 40 }, (_, index) => ({
  id: String(index + 1),
}));

function CardList() {
  const { filter, setFilter } = useFilterState({
    filterMode: FilterMode.Scenes,
  });
  const mobile = useMediaQuery("(max-width: 767px)");
  const selection = useListSelect(items);
  return (
    <EntityList
      sidebarState={{
        showSidebar: false,
        sectionOpen: {},
        setSectionOpen: () => {},
        isMobileSidebar: mobile,
        closeFilterSidebar: () => {},
        openFilterSidebar: () => {},
      }}
      filter={filter}
      setFilter={setFilter}
      listSelect={selection}
      totalCount={80}
      activeFilterCount={0}
      sidebarContent={null}
    >
      <CardAspectContext value="landscape">
        <VirtualizedItemList
          displayMode={DisplayMode.Grid}
          mobileGridCols={2}
          zoomIndex={2}
          isMobile={mobile}
          isLoading={false}
          itemsPerPage={40}
          preserveScrollDuringRefill={false}
          items={items}
          selectedIds={selection.selectedIds}
          onSelectChange={selection.onSelectChange}
          renderCard={(item, isMobile, selected, onSelectedChanged) => (
            <div data-card-id={item.id} data-card-page={filter.currentPage}>
              <EntityCard
                id={item.id}
                label={`Card ${item.id}`}
                destination={{
                  to: "/scenes/$sceneId",
                  params: { sceneId: item.id },
                  search: undefined,
                }}
                isMobile={isMobile}
                selected={selected}
                onSelectedChanged={onSelectedChanged}
              >
                <EntityCard.Preview
                  image={`/fixture-cover/legacy-${item.id}.svg`}
                  naturalIsPortrait
                  previewImage={{
                    fallback: `/fixture-cover/full-${item.id}.svg`,
                    sources: [],
                    thumbnail: {
                      fallback: `/fixture-cover/thumbnail-fallback-${item.id}.svg`,
                      sources: [
                        {
                          url: `/fixture-cover/thumbnail-${item.id}.svg`,
                          mime_type: "image/svg+xml",
                          dynamic_range: PreviewImageDynamicRange.Adaptive,
                          width: 1280,
                          height: 720,
                        },
                      ],
                    },
                  }}
                />
                <EntityCard.Body>
                  <EntityCard.Title>Card {item.id}</EntityCard.Title>
                </EntityCard.Body>
              </EntityCard>
            </div>
          )}
        />
      </CardAspectContext>
    </EntityList>
  );
}

export function EmbeddedListFixture() {
  const [tab, setTab] = useState("scenes");
  const [headerHeight, setHeaderHeight] = useState(900);
  return (
    <CollectionDetailLayout title="Large collection" onBack={() => {}}>
      <div className="md:flex md:h-full">
        <aside className="md:w-72 md:shrink-0">
          <div data-testid="collection-header" style={{ height: headerHeight }}>
            <Button onClick={() => setHeaderHeight(1300)}>Grow header</Button>
          </div>
        </aside>
        <DetailTabs
          activeTab={tab}
          onTabChange={setTab}
          tabs={[
            { id: "scenes", label: "Scenes", content: <CardList /> },
            {
              id: "other",
              label: "Other",
              content: <div className="h-24">Other content</div>,
            },
          ]}
        />
      </div>
    </CollectionDetailLayout>
  );
}
