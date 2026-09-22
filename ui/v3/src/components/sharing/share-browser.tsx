import {
  useCallback,
  useLayoutEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useIntl } from "react-intl";
import { ArrowLeft, Clock, Images } from "lucide-react";
import { EntityCard } from "@/components/cards/entity-card";
import { VirtualizedItemList } from "@/components/list/virtualized-item-list";
import { ListScrollContext } from "@/components/list/list-scroll-context";
import { CardAspectContext } from "@/components/list/card-aspect-context";
import { MobileGridColsContext } from "@/components/list/mobile-grid-context";
import { DisplayMode } from "@/models/list-filter/types";
import { useMediaQuery } from "@/utils/screen";
import { Tabs, TabsContent } from "@/components/ui/tabs";
import { DetailTabStrip } from "@/components/detail/detail-tab-strip";
import { Button } from "@/components/ui/button";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { SharedMediaViewer } from "./shared-media-viewer";
import { SharedLightbox } from "./shared-lightbox";
import type { SharedContent, SharedMedia } from "./share-contract";
import {
  shareCollections,
  shareLocationURL,
  shareTabSchema,
  useShareNavigation,
  type SharedItem,
  type ShareLocation,
  type ShareTab,
} from "./share-browser-state";

const tabLabels = {
  scenes: { id: "scenes", defaultMessage: "Scenes" },
  images: { id: "images", defaultMessage: "Images" },
  galleries: { id: "galleries", defaultMessage: "Galleries" },
};
const kindLabels = {
  SCENE: { id: "scene", defaultMessage: "Scene" },
  IMAGE: { id: "image", defaultMessage: "Image" },
  GALLERY: { id: "gallery", defaultMessage: "Gallery" },
};

/** Reuse the library's virtual grid and cards, with share-local navigation. */
function SharedGrid({
  items,
  base,
  location,
  navigate,
  onPreview,
}: {
  items: SharedItem[];
  base: URL;
  location: ShareLocation;
  navigate: (next: ShareLocation) => void;
  onPreview: (item: SharedMedia) => void;
}) {
  const intl = useIntl();
  const isMobile = useMediaQuery("(max-width: 767px)");
  const renderCard = useCallback(
    (item: SharedItem, mobile: boolean) => {
      const title = item.title || intl.formatMessage(kindLabels[item.kind]);
      const gallery = item.kind === "GALLERY";
      const preview = gallery ? item.media[0] : item;
      const next: ShareLocation = gallery
        ? { tab: "galleries", gallery: item.id }
        : { ...location, media: item.key };
      return (
        <EntityCard
          id={item.id}
          label={title}
          isMobile={mobile}
          href={shareLocationURL(base, next)}
          onNavigate={() => navigate(next)}
          onPreviewClick={gallery ? undefined : () => onPreview(item)}
        >
          <EntityCard.Preview
            image={preview?.thumbnail}
            duration={!gallery && item.video ? item.duration : undefined}
            isPortrait={item.kind === "IMAGE" && item.height > item.width}
            naturalIsPortrait={preview ? preview.height > preview.width : false}
          />
          <EntityCard.Body>
            <EntityCard.Title>{title}</EntityCard.Title>
            {gallery && (
              <EntityCard.Subtitle noTooltip>
                {intl.formatMessage(
                  {
                    id: "sharing.item_count",
                    defaultMessage:
                      "{count, plural, one {# item} other {# items}}",
                  },
                  { count: item.media.length },
                )}
              </EntityCard.Subtitle>
            )}
          </EntityCard.Body>
        </EntityCard>
      );
    },
    [intl, base, location, navigate, onPreview],
  );
  if (!items.length)
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>
            {intl.formatMessage({
              id: "sharing.empty",
              defaultMessage: "No shared media",
            })}
          </EmptyTitle>
        </EmptyHeader>
      </Empty>
    );
  return (
    <CardAspectContext value="landscape">
      <MobileGridColsContext value={2}>
        <VirtualizedItemList
          displayMode={DisplayMode.Grid}
          mobileGridCols={2}
          zoomIndex={2}
          isMobile={isMobile}
          isLoading={false}
          itemsPerPage={items.length}
          preserveScrollDuringRefill={false}
          items={items}
          renderCard={renderCard}
        />
      </MobileGridColsContext>
    </CardAspectContext>
  );
}

function SharedScroll({
  viewKey,
  positions,
  children,
}: {
  viewKey: string;
  positions: Map<string, number>;
  children: ReactNode;
}) {
  const [element, setElement] = useState<HTMLDivElement | null>(null);
  const [initialOffset] = useState(() => positions.get(viewKey) ?? 0);
  useLayoutEffect(() => {
    if (!element) return;
    element.scrollTop = initialOffset;
    return () => {
      positions.set(viewKey, element.scrollTop);
    };
  }, [element, initialOffset, positions, viewKey]);
  const context = useMemo(
    () => ({
      element,
      initialOffset,
      restorationId: "share",
      restorationKey: viewKey,
    }),
    [element, initialOffset, viewKey],
  );
  return (
    <div
      ref={setElement}
      data-share-scroll
      className="min-h-0 flex-1 overflow-y-auto overscroll-contain pb-[max(1rem,env(safe-area-inset-bottom))]"
    >
      <ListScrollContext value={context}>{children}</ListScrollContext>
    </div>
  );
}

export function ShareBrowser({
  content,
  base,
}: {
  content: SharedContent;
  base: URL;
}) {
  const intl = useIntl();
  const collections = useMemo(() => shareCollections(content), [content]);
  const { location, navigate, back } = useShareNavigation(base);
  const tabs: ShareTab[] = shareTabSchema.options.filter(
    (tab) => collections[tab].length > 0,
  );
  const tab =
    location.tab && tabs.includes(location.tab)
      ? location.tab
      : (tabs[0] ?? "scenes");
  const gallery = location.gallery
    ? collections.galleries.find((item) => item.id === location.gallery)
    : undefined;
  const media = location.media
    ? collections.byKey.get(location.media)
    : undefined;
  const unavailable =
    (location.gallery && !gallery) ||
    (location.media &&
      (!media ||
        (gallery && !gallery.media.some((item) => item.key === media.key))));
  const listLocation: ShareLocation = {
    tab,
    ...(gallery && { gallery: gallery.id }),
  };
  const parent: ShareLocation = media ? listLocation : { tab };
  const current = media ?? gallery;
  const title = current
    ? current.title || intl.formatMessage(kindLabels[current.kind])
    : undefined;
  const items = gallery?.media ?? collections[tab];
  const [positions] = useState(() => new Map<string, number>());
  const [lightbox, setLightbox] = useState<{
    media: SharedMedia[];
    selectedKey: string;
  } | null>(null);
  const openViewer = useCallback(
    (item: SharedMedia) => {
      const available =
        gallery?.media ??
        (item.kind === "SCENE" ? collections.scenes : collections.images);
      setLightbox({ media: available, selectedKey: item.key });
    },
    [gallery, collections],
  );
  const closeViewer = useCallback(() => setLightbox(null), []);
  const viewKey = shareLocationURL(base, { ...location, tab });
  const grid = (
    <SharedGrid
      items={items}
      base={base}
      location={listLocation}
      navigate={navigate}
      onPreview={openViewer}
    />
  );
  return (
    <main className="mx-auto flex h-dvh w-full max-w-7xl min-w-0 flex-col overflow-hidden pt-[env(safe-area-inset-top)]">
      <header className="flex shrink-0 flex-wrap items-center justify-between gap-x-4 gap-y-1 px-4 py-4 sm:px-6">
        <h1 className="min-w-0 break-words text-xl font-semibold [overflow-wrap:anywhere]">
          {content.label}
        </h1>
        <p className="flex items-center gap-2 text-xs text-muted-foreground">
          <Clock className="size-3.5 shrink-0" />
          {intl.formatMessage(
            { id: "sharing.expires_on", defaultMessage: "Expires {date}" },
            {
              date: intl.formatDate(content.expires_at, {
                dateStyle: "medium",
                timeStyle: "short",
              }),
            },
          )}
        </p>
      </header>
      {location.media || location.gallery ? (
        <>
          <div className="flex shrink-0 items-center gap-2 border-y px-2 py-1 sm:px-4">
            <Button
              variant="ghost"
              size="icon"
              onClick={() => back(parent)}
              aria-label={intl.formatMessage({
                id: "actions.back",
                defaultMessage: "Back",
              })}
            >
              <ArrowLeft />
            </Button>
            {gallery && !media && <Images className="size-4 shrink-0" />}
            <h2 className="min-w-0 truncate font-medium">{title}</h2>
          </div>
          <SharedScroll key={viewKey} viewKey={viewKey} positions={positions}>
            {unavailable ? (
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>
                    {intl.formatMessage({
                      id: "sharing.media_unavailable",
                      defaultMessage: "This item is no longer available.",
                    })}
                  </EmptyTitle>
                </EmptyHeader>
              </Empty>
            ) : media ? (
              <div className="p-3 sm:p-6">
                <SharedMediaViewer
                  media={media}
                  base={base}
                  suspended={!!lightbox}
                  onOpenViewer={() => openViewer(media)}
                />
              </div>
            ) : (
              grid
            )}
          </SharedScroll>
        </>
      ) : (
        <Tabs
          value={tab}
          onValueChange={(value) => {
            const parsed = shareTabSchema.safeParse(value);
            if (parsed.success) navigate({ tab: parsed.data });
          }}
          className="min-h-0 flex-1 gap-0"
        >
          <DetailTabStrip
            className="mx-4 mb-2 w-fit max-w-[calc(100%-2rem)]"
            ariaLabel={intl.formatMessage({
              id: "sharing.media_types",
              defaultMessage: "Shared media types",
            })}
            tabs={(tabs.length ? tabs : [tab]).map((value) => ({
              id: value,
              label: `${intl.formatMessage(tabLabels[value])} (${intl.formatNumber(collections[value].length)})`,
            }))}
          />
          <TabsContent value={tab} className="flex min-h-0 flex-1 flex-col">
            <SharedScroll key={viewKey} viewKey={viewKey} positions={positions}>
              {grid}
            </SharedScroll>
          </TabsContent>
        </Tabs>
      )}
      {lightbox && (
        <SharedLightbox {...lightbox} base={base} onClose={closeViewer} />
      )}
    </main>
  );
}
