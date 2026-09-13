import { Pencil, Wand2, RotateCw, RotateCcw } from "lucide-react";
import {
  EntityActionButton,
  EntityActionsMenu,
} from "@/components/detail/entity-actions-menu";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { StrictMode, useState } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
  useRouterState,
} from "@tanstack/react-router";
import { ShortcutProvider } from "@/components/shortcut-provider";
import { MobileListBar } from "@/components/list/mobile-list-bar";
import { useListSelect } from "@/components/list/use-list-select";
import { ListFilterModel } from "@/models/list-filter/filter";
import { FilterMode } from "@/core/generated-graphql";
import { Button } from "@/components/ui/button";
import { CollectionDetailLayout } from "@/components/detail/collection-detail-layout";
import { MediaDetailLayout } from "@/components/detail/media-detail-layout";
import {
  MobileDetailChromePortal,
  useMobileDetailChrome,
} from "@/components/layout/mobile-detail-chrome";
import { DetailTabs } from "@/components/detail/detail-tabs";
import { DetailEditTransition } from "@/components/detail/detail-edit-transition";
import { EntityList } from "@/components/list/entity-list";
import { useMediaQuery } from "@/utils/screen";
import { SettingsLayout } from "@/components/settings/settings-layout";
import { SETTINGS_NAV_ITEMS } from "@/components/settings/settings-navigation";
import { Input } from "@/components/ui/input";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
import "./style.css";
import { PlayerFixture } from "./player";
import { VideoSourcesFixture } from "./video-sources";
import { SceneLightboxFixture } from "./scene-lightbox";
import { SceneDetailFixture } from "./scene-detail";
import { EntityCardsFixture } from "./entity-cards";

const params = new URLSearchParams(location.search);
const items = Array.from({ length: 40 }, (_, index) => ({
  id: String(index + 1),
}));
const sections = [
  { id: "scenes", label: "Scenes" },
  { id: "images", label: "Images" },
  { id: "galleries", label: "Galleries" },
  { id: "groups", label: "Groups" },
];

function FixtureList({ name }: { name: string }) {
  const [filter, setFilter] = useState(
    () => new ListFilterModel(FilterMode.Performers),
  );
  const [filterOpen, setFilterOpen] = useState(false);
  const [cols, setCols] = useState<1 | 2>(2);
  const selection = useListSelect(items);
  const smallScreen = useMediaQuery("(max-width: 767px)");
  const mobile = useMobileDetailChrome()?.mobile ?? smallScreen;
  const [totalCount, setTotalCount] = useState(params.has("single") ? 10 : 200);
  const sidebarState = {
    showSidebar: filterOpen,
    sectionOpen: {},
    setSectionOpen: () => {},
    isMobileSidebar: mobile,
    closeFilterSidebar: () => setFilterOpen(false),
    openFilterSidebar: () => setFilterOpen(true),
  };
  return (
    <EntityList
      sidebarState={sidebarState}
      filter={filter}
      setFilter={setFilter}
      listSelect={selection}
      activeFilterCount={0}
      totalCount={totalCount}
      sidebarContent={<p>Example filters</p>}
      mobileChrome={
        <MobileListBar
          filter={filter}
          setFilter={setFilter}
          totalCount={totalCount}
          activeFilterCount={0}
          hasSelection={selection.hasSelection}
          selecting={selection.selecting}
          selectedCount={selection.selectedItems.length}
          onSelectAll={selection.onSelectAll}
          onSelectNone={selection.onSelectNone}
          openFilterSidebar={() => setFilterOpen(true)}
          mobileGridCols={cols}
          setMobileGridCols={setCols}
        />
      }
    >
      <div data-testid={`${name}-list`} className="p-3">
        <p
          data-testid="list-state"
          data-term={filter.searchTerm ?? ""}
          data-sort={filter.sortBy}
          data-direction={filter.sortDirection}
        >
          {name} page {filter.currentPage}: {filter.searchTerm}
        </p>
        <Button onClick={selection.onEnterSelect}>Enter selection</Button>
        <Button onClick={() => setTotalCount(80)}>Reduce results</Button>
        <div
          className="mt-3 grid gap-2"
          style={{ gridTemplateColumns: `repeat(${cols}, 1fr)` }}
        >
          {items.map((item) => (
            <Button
              key={item.id}
              variant={
                selection.selectedIds.has(item.id) ? "secondary" : "outline"
              }
              className="h-28"
              onClick={() =>
                selection.onSelectChange(
                  item.id,
                  !selection.selectedIds.has(item.id),
                  false,
                )
              }
            >
              {name} {item.id}
            </Button>
          ))}
        </div>
      </div>
    </EntityList>
  );
}

function FixturePage() {
  const [tab, setTab] = useState("scenes");
  const [editing, setEditing] = useState(false);
  const [favourite, setFavourite] = useState(false);
  const [backCount, setBackCount] = useState(0);
  const [actionOpen, setActionOpen] = useState(false);
  const tabs = sections.map((section) => ({
    ...section,
    content: <FixtureList name={section.id} />,
  }));
  const toolbar = (
    <div className="flex flex-wrap gap-2">
      <Button onClick={() => setFavourite(!favourite)}>
        {favourite ? "Favourited" : "Favourite"}
      </Button>
      <EntityActionButton
        icon={Pencil}
        label="Edit"
        onClick={() => setEditing(true)}
      />
      <EntityActionsMenu
        items={[
          {
            key: "auto-tag",
            icon: Wand2,
            label: "Auto tag…",
            onSelect: () => setActionOpen(true),
          },
          {
            key: "rotation",
            icon: RotateCw,
            label: "Rotation",
            actions: [
              {
                key: "clockwise",
                icon: RotateCw,
                label: "Rotate clockwise",
                onSelect: () => setActionOpen(true),
              },
              {
                key: "counter-clockwise",
                icon: RotateCcw,
                label: "Rotate counter-clockwise",
                onSelect: () => setActionOpen(true),
              },
            ],
          },
        ]}
      />
      <Dialog open={actionOpen} onOpenChange={setActionOpen}>
        <DialogContent>
          <DialogTitle>Action form</DialogTitle>
          <Button onClick={() => setActionOpen(false)}>Cancel action</Button>
        </DialogContent>
      </Dialog>
    </div>
  );
  return (
    <div
      data-app-viewport
      data-testid="viewport"
      data-back-count={backCount}
      className="flex h-dvh flex-col overflow-hidden"
    >
      {params.has("standalone") ? (
        <FixtureList name="standalone" />
      ) : params.has("media") ? (
        <MediaDetailLayout
          title="Example scene"
          tabs={tabs}
          primaryContent={<div className="h-96">Example video area</div>}
          headerContent={toolbar}
          mobilePageScroll
          onBack={() => setBackCount(backCount + 1)}
        />
      ) : (
        <CollectionDetailLayout
          title="Example performer"
          onBack={() => setBackCount(backCount + 1)}
        >
          <div className="md:flex md:h-full">
            <aside className="md:w-72 md:shrink-0">
              <DetailEditTransition
                editing={editing}
                detail={
                  <>
                    <div className="h-80 p-3">Entity information</div>
                    <MobileDetailChromePortal slot="actions">
                      {toolbar}
                    </MobileDetailChromePortal>
                  </>
                }
                editForm={
                  <div className="p-3">
                    <p>Editor</p>
                    <Button onClick={() => setEditing(false)}>
                      Close editor
                    </Button>
                  </div>
                }
              />
            </aside>
            <DetailTabs tabs={tabs} activeTab={tab} onTabChange={setTab} />
          </div>
        </CollectionDetailLayout>
      )}
    </div>
  );
}

function FixtureSettingsPage() {
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  });
  const [draft, setDraft] = useState("");
  return (
    <div className="p-6">
      <p data-testid="settings-page">{pathname}</p>
      <Input
        aria-label="Unsaved setting"
        value={draft}
        onChange={(event) => setDraft(event.currentTarget.value)}
      />
      <div className="h-400">Settings content</div>
      <p>Last setting</p>
    </div>
  );
}

const rootRoute = createRootRoute({ component: Outlet });
const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/settings",
  validateSearch: (search: Record<string, unknown>) => ({
    hl: typeof search.hl === "string" ? search.hl : undefined,
  }),
  component: () => (
    <div data-app-viewport className="flex h-dvh flex-col overflow-hidden">
      <SettingsLayout>
        <Outlet />
      </SettingsLayout>
    </div>
  ),
});
const router = createRouter({
  routeTree: rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: FixturePage,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/player",
      component: PlayerFixture,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/video-sources",
      component: VideoSourcesFixture,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/scene-lightbox",
      component: SceneLightboxFixture,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/scene-detail",
      component: SceneDetailFixture,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/entity-cards",
      component: EntityCardsFixture,
    }),
    settingsRoute.addChildren(
      SETTINGS_NAV_ITEMS.map((item) =>
        createRoute({
          getParentRoute: () => settingsRoute,
          path: item.to.slice("/settings/".length),
          component: FixtureSettingsPage,
        }),
      ),
    ),
  ]),
  scrollRestoration: true,
});
const root = document.getElementById("root");
if (!root) throw new Error("Missing fixture root");
createRoot(root).render(
  <StrictMode>
    <IntlProvider locale="en-GB" messages={flattenMessages(messages)}>
      <ShortcutProvider>
        <RouterProvider router={router} />
      </ShortcutProvider>
    </IntlProvider>
  </StrictMode>,
);
