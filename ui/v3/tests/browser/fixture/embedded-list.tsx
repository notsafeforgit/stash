import { useState } from "react";
import { MockedProvider } from "@apollo/client/testing/react";
import { EntityCard } from "@/components/cards/entity-card";
import { SceneCard, SceneCardLayout } from "@/components/cards/scene-card";
import { CollectionDetailLayout } from "@/components/detail/collection-detail-layout";
import { DetailTabs } from "@/components/detail/detail-tabs";
import { EntityList } from "@/components/list/entity-list";
import { CardAspectContext } from "@/components/list/card-aspect-context";
import { MobileGridColsContext } from "@/components/list/mobile-grid-context";
import { CardLayoutContext } from "@/components/list/card-layout-context";
import { VirtualizedItemList } from "@/components/list/virtualized-item-list";
import { useListSelect } from "@/components/list/use-list-select";
import { useFilterState } from "@/components/list/use-filter-state";
import { Button } from "@/components/ui/button";
import {
  FilterMode,
  PreviewImageDynamicRange,
  ServerCapabilitiesDocument,
} from "@/core/generated-graphql";
import { DisplayMode } from "@/models/list-filter/types";
import { useMediaQuery } from "@/utils/screen";
import { scenes } from "./scene-lightbox";

const items = Array.from({ length: 40 }, (_, index) => ({
  id: String(index + 1),
}));
const params = new URLSearchParams(location.search);
const mixed = params.has("mixed");
const columns = mixed && !params.has("two-columns") ? 1 : 2;
const aspect = params.has("portrait")
  ? "portrait"
  : params.has("landscape")
    ? "landscape"
    : mixed
      ? "auto"
      : "landscape";
const layout = params.has("details") ? "details" : "grid";
const portrait = (id: string) => mixed && Number(id) % 3 !== 0;
const title = (id: string) =>
  mixed
    ? `Card ${id}: A long title that wraps onto another line`
    : `Card ${id}`;
const details = (id: string) =>
  "A description with enough text to exercise wrapping at narrow widths. ".repeat(
    Number(id) % 4,
  );

function CardBody({ id }: { id: string }) {
  return (
    <EntityCard.Body>
      <EntityCard.Title>{title(id)}</EntityCard.Title>
      {mixed && <EntityCard.Subtitle>{details(id)}</EntityCard.Subtitle>}
    </EntityCard.Body>
  );
}

const scene = scenes[0];
if (!scene) throw new Error("Missing scene fixture");
const mixedItems = items.map(({ id }) => ({
  ...scene,
  id,
  title: title(id),
  details: details(id),
  paths: {
    ...scene.paths,
    screenshot: `/fixture-cover/thumbnail-${id}.svg`,
    preview: "",
    webp: "",
    vtt: "",
  },
  preview_image: null,
  files: scene.files.map((file) => ({
    ...file,
    width: portrait(id) ? 720 : 1280,
    height: portrait(id) ? 1280 : 720,
  })),
  tags: Array.from({ length: Number(id) % 6 }, (_, index) => ({
    __typename: "Tag" as const,
    id: String(index),
    name: `A longer tag ${index}`,
  })),
}));
const cardItems = items.map((item, index) => ({
  ...item,
  scene: mixed ? mixedItems[index] : undefined,
}));
const renderCardLayout = (item: (typeof items)[number], isMobile: boolean) => {
  const scene = mixedItems.find((scene) => scene.id === item.id);
  return mixed && scene ? (
    <SceneCardLayout scene={scene} isMobile={isMobile} />
  ) : (
    <EntityCard.Layout isMobile={isMobile}>
      <CardBody id={item.id} />
    </EntityCard.Layout>
  );
};

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
      <CardAspectContext value={aspect}>
        <MobileGridColsContext value={columns}>
          <CardLayoutContext value={layout}>
            <VirtualizedItemList
              displayMode={
                layout === "details" ? DisplayMode.Details : DisplayMode.Grid
              }
              mobileGridCols={columns}
              zoomIndex={2}
              isMobile={mobile}
              isLoading={false}
              itemsPerPage={40}
              preserveScrollDuringRefill={false}
              items={cardItems}
              renderCardLayout={renderCardLayout}
              selectedIds={selection.selectedIds}
              onSelectChange={selection.onSelectChange}
              renderCard={(item, isMobile, selected, onSelectedChanged) => (
                <div data-card-id={item.id} data-card-page={filter.currentPage}>
                  {item.scene ? (
                    <SceneCard
                      scene={item.scene}
                      isMobile={isMobile}
                      selected={selected}
                      onSelectedChanged={onSelectedChanged}
                      destination={{
                        to: "/scenes/$sceneId",
                        params: { sceneId: item.id },
                        search: undefined,
                      }}
                    />
                  ) : (
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
                        isPortrait={portrait(item.id)}
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
                                dynamic_range:
                                  PreviewImageDynamicRange.Adaptive,
                                width: 1280,
                                height: 720,
                              },
                            ],
                          },
                        }}
                      />
                      <CardBody id={item.id} />
                    </EntityCard>
                  )}
                </div>
              )}
            />
          </CardLayoutContext>
        </MobileGridColsContext>
      </CardAspectContext>
    </EntityList>
  );
}

export function EmbeddedListFixture() {
  const [tab, setTab] = useState("scenes");
  const [headerHeight, setHeaderHeight] = useState(900);
  return (
    <MockedProvider
      mocks={[
        {
          request: { query: ServerCapabilitiesDocument },
          result: { data: { serverCapabilities: { downloadFormats: [] } } },
          maxUsageCount: Infinity,
          delay: 0,
        },
      ]}
    >
      <CollectionDetailLayout title="Large collection" onBack={() => {}}>
        <div className="md:flex md:h-full">
          <aside className="md:w-72 md:shrink-0">
            <div
              data-testid="collection-header"
              style={{ height: headerHeight }}
            >
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
    </MockedProvider>
  );
}
