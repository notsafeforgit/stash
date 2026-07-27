import React, { useCallback, useRef, useState } from "react";
import {
  createFileRoute,
  useNavigate,
  useRouter,
} from "@tanstack/react-router";
import { useSmartBack } from "src/hooks/use-smart-back";
import { useQuery, useMutation } from "@apollo/client/react";
import { zodValidator } from "@tanstack/zod-adapter";
import { z } from "zod";
import { useIntl } from "react-intl";
import { Spinner } from "src/components/ui/spinner";
import {
  Star,
  Droplets,
  Play,
  CheckCircle2Icon,
  Pencil,
  ChevronLeft,
} from "lucide-react";
import { Button } from "src/components/ui/button";
import { cn } from "src/lib/utils";
import * as GQL from "src/core/generated-graphql";
import { objectTitle } from "src/core/files";
import { ScenePlayer } from "src/components/player/scene-player";
import { useConfigurationContext } from "src/hooks/config";
import { useMediaQuery } from "src/utils/screen";
import {
  MediaDetailLayout,
  type DetailTab,
} from "src/components/detail/media-detail-layout";
import {
  SceneDetailsTab,
  SceneFileInfoTab,
  SceneGalleriesTab,
  SceneGroupsTab,
  SceneHistoryTab,
} from "src/components/detail/scene-detail-tabs";
import { SceneMarkersTab } from "src/components/detail/scene-markers-tab";
import type {
  MarkerBoundary,
  MarkerFormBounds,
} from "src/components/detail/marker-edit-form";
import { SceneVideoFilterTab } from "src/components/detail/scene-video-filter";
import { SceneEditForm } from "src/components/detail/scene-edit-form";
import { SceneActionsMenu } from "src/components/detail/scene-actions-menu";
import { DetailEditTransition } from "src/components/detail/detail-edit-transition";
import { useDocumentTitle } from "src/hooks/title";

// ── Route search params ────────────────────────────────────────────────────────

const searchSchema = z.object({
  /** Start at this timestamp in seconds */
  t: z.number().optional(),
  /** Auto-play on load */
  autoplay: z.boolean().optional(),
  /** Active tab */
  tab: z.string().optional(),
});

// ── Scene toolbar ──────────────────────────────────────────────────────────────

type SceneData = NonNullable<GQL.FindSceneQuery["findScene"]>;

interface SceneToolbarProps {
  scene: SceneData;
  onAddO: () => void;
  onAddPlay: () => void;
  onToggleOrganized: () => void;
  getPlayerPosition?: () => number | undefined;
  onDeleted?: () => void;
  onScreenshotGenerated?: () => void | Promise<void>;
}

function SceneToolbar({
  scene,
  onAddO,
  onAddPlay,
  onToggleOrganized,
  getPlayerPosition,
  onDeleted,
  onScreenshotGenerated,
}: SceneToolbarProps) {
  const intl = useIntl();

  return (
    <div className="flex items-center gap-3 py-1.5 flex-wrap">
      <div className="flex items-center flex-wrap gap-2">
        {scene.rating100 != null && (
          <span
            className="inline-flex items-center bg-transparent border border-border rounded-md text-muted-foreground text-[0.8125rem] gap-1 px-2 py-1"
            title={intl.formatMessage({
              id: "rating",
              defaultMessage: "Rating",
            })}
          >
            <Star size={14} />
            {scene.rating100}
          </span>
        )}
        {scene.play_count != null && (
          <Button
            variant="outline"
            className="h-auto bg-transparent px-2 py-1 text-[0.8125rem] gap-1 text-muted-foreground hover:text-foreground"
            onClick={onAddPlay}
            title={intl.formatMessage({
              id: "play_count",
              defaultMessage: "Add play",
            })}
          >
            <Play size={14} />
            {scene.play_count}
          </Button>
        )}
        {scene.o_counter != null && (
          <Button
            variant="outline"
            className="h-auto bg-transparent px-2 py-1 text-[0.8125rem] gap-1 text-muted-foreground hover:text-foreground"
            onClick={onAddO}
            title={intl.formatMessage({
              id: "o_counter",
              defaultMessage: "Add O",
            })}
          >
            <Droplets size={14} />
            {scene.o_counter}
          </Button>
        )}
      </div>

      <Button
        variant="outline"
        size="sm"
        onClick={onToggleOrganized}
        className={cn(
          scene.organized &&
            "text-green-600 border-green-500/60 hover:text-green-500",
        )}
        title={intl.formatMessage({
          id: "organized",
          defaultMessage: "Organized",
        })}
      >
        <CheckCircle2Icon
          size={13}
          className={scene.organized ? "fill-green-600/20" : ""}
        />
        {intl.formatMessage({ id: "organized", defaultMessage: "Organized" })}
      </Button>

      <div className="ml-auto">
        <SceneActionsMenu
          scene={scene}
          getPlayerPosition={getPlayerPosition}
          onDeleted={onDeleted}
          onScreenshotGenerated={onScreenshotGenerated}
        />
      </div>
    </div>
  );
}

// ── Scene detail page ─────────────────────────────────────────────────────────

function SceneDetailPage() {
  const { sceneId } = Route.useParams();
  const { t, autoplay, tab } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const router = useRouter();
  const goBack = useSmartBack("/scenes");
  const intl = useIntl();

  const setTimestampRef = useRef<((t: number) => void) | null>(null);
  const sendSetTimestamp = useCallback((setter: (t: number) => void) => {
    setTimestampRef.current = setter;
  }, []);

  const getCurrentTimeRef = useRef<(() => number | undefined) | null>(null);
  const sendGetCurrentTime = useCallback((getter: () => number | undefined) => {
    getCurrentTimeRef.current = getter;
  }, []);
  const getPlayerPosition = useCallback(
    () => getCurrentTimeRef.current?.(),
    [],
  );

  // ── Marker editor coordination ──
  // The marker edit sheet (rendered inside `SceneMarkersTab`) emits its
  // current `start` / `end` form values up here on every change; we feed
  // them straight back into `<ScenePlayer clipBoundsEdit>` so the
  // position-slider's draggable handles always sit at the in-flight
  // values. Conversely, the form registers a writer in `boundSetterRef`
  // — when the user drags a slider handle, the player's onChange routes
  // through `handleClipBoundDrag`, which calls back into the form.
  // Bounds are reset to `null` on the form's unmount (sheet close), at
  // which point the player drops the handles.
  const [editBounds, setEditBounds] = useState<MarkerFormBounds | null>(null);
  const boundSetterRef = useRef<
    ((boundary: MarkerBoundary, time: number) => void) | null
  >(null);
  const registerBoundSetter = useCallback(
    (setter: (boundary: MarkerBoundary, time: number) => void) => {
      boundSetterRef.current = setter;
    },
    [],
  );
  const handleBoundsChange = useCallback((bounds: MarkerFormBounds) => {
    // Both null = sheet closed. Drop the player's handles.
    if (bounds.start == null && bounds.end == null) {
      setEditBounds(null);
    } else {
      setEditBounds(bounds);
    }
  }, []);
  const handleClipBoundDrag = useCallback(
    (next: { start?: number | null; end?: number | null }) => {
      if (next.start != null) boundSetterRef.current?.("start", next.start);
      if (next.end != null) boundSetterRef.current?.("end", next.end);
    },
    [],
  );

  // Pinch-zoom is desktop-only on the detail page. On mobile the page
  // itself scrolls (mobilePageScroll), so two-finger gestures inside the
  // player would compete with page scroll and feel unpredictable. The
  // lightbox slide (separate consumer of ScenePlayer) keeps pinch-zoom
  // because it owns the viewport. `lg:` is Tailwind's 1024px breakpoint,
  // matching the same media query the surrounding layout uses.
  const isDesktop = useMediaQuery("(min-width: 1024px)");

  const { configuration } = useConfigurationContext();
  const autostartEnabled = configuration.interface.autostartVideo ?? true;

  const { data, loading, error, refetch } = useQuery(GQL.FindSceneDocument, {
    variables: { id: sceneId },
    fetchPolicy: "cache-first",
  });

  const [addO] = useMutation(GQL.SceneAddODocument, {
    variables: { id: sceneId },
    // Optimistic update for o_counter
    update(cache, { data: result }) {
      if (!result?.sceneAddO) return;
      cache.modify({
        id: cache.identify({ __typename: "Scene", id: sceneId }),
        fields: {
          o_counter: () => result.sceneAddO.count,
          o_history: () => result.sceneAddO.history,
        },
      });
    },
  });

  const [addPlay] = useMutation(GQL.SceneAddPlayDocument, {
    variables: { id: sceneId },
    update(cache, { data: result }) {
      if (!result?.sceneAddPlay) return;
      cache.modify({
        id: cache.identify({ __typename: "Scene", id: sceneId }),
        fields: {
          play_count: () => result.sceneAddPlay.count,
          play_history: () => result.sceneAddPlay.history,
        },
      });
    },
  });

  const [saveActivity] = useMutation(GQL.SceneSaveActivityDocument);

  const [updateScene] = useMutation(GQL.SceneUpdateDocument);
  function handleToggleOrganized() {
    if (!scene) return;
    updateScene({
      variables: { input: { id: sceneId, organized: !scene.organized } },
      optimisticResponse: {
        sceneUpdate: { ...scene, organized: !scene.organized },
      },
    });
  }

  function handleOnEnded() {
    // Save final play activity on video end
    if (!scene) return;
    const file = scene.files[0];
    if (!file) return;
    saveActivity({
      variables: {
        id: sceneId,
        resume_time: 0,
        playDuration: file.duration,
      },
    });
    addPlay({ variables: { id: sceneId } });
  }

  function handleSeek(seconds: number) {
    setTimestampRef.current?.(seconds);
  }

  const activeTab = tab ?? "details";
  function setActiveTab(id: string) {
    navigate({
      search: (prev) => ({ ...prev, tab: id }),
      state: router.state.location.state,
      replace: true,
    });
  }

  // The Details tab houses an inline transition between the read-only
  // details view and the edit form. Replaces the previous "Edit" tab —
  // an Edit button at the top of the details panel toggles into edit
  // mode, the form's header has a Back button to toggle out. Reset to
  // false whenever the user navigates to a different tab so re-opening
  // Details lands on the read-only view.
  const [editingDetails, setEditingDetails] = useState(false);
  React.useEffect(() => {
    if (activeTab !== "details") setEditingDetails(false);
  }, [activeTab]);

  const scene = data?.findScene;
  useDocumentTitle(scene ? objectTitle(scene) || undefined : undefined);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-48 text-muted-foreground">
        <Spinner className="size-10" />
      </div>
    );
  }

  if (error || !scene) {
    return (
      <div className="p-4 text-destructive">
        {error?.message ??
          intl.formatMessage({
            id: "scene_not_found",
            defaultMessage: "Scene not found",
          })}
      </div>
    );
  }

  const tabs: DetailTab[] = [
    {
      id: "details",
      label: intl.formatMessage({ id: "details", defaultMessage: "Details" }),
      shortcut: "a",
      content: (
        <DetailEditTransition
          editing={editingDetails}
          detail={
            <div className="flex flex-col gap-3">
              <div className="flex justify-end">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setEditingDetails(true)}
                >
                  <Pencil size={13} />
                  {intl.formatMessage({
                    id: "actions.edit",
                    defaultMessage: "Edit",
                  })}
                </Button>
              </div>
              <SceneDetailsTab scene={scene} />
            </div>
          }
          editForm={
            <div className="flex flex-col h-full">
              {/* Header sized to match `DetailSidebarBack`. */}
              <div className="flex shrink-0 items-center gap-1 px-1 py-1 border-b border-border">
                <Button
                  variant="ghost"
                  size="sm"
                  className="px-2 shrink-0"
                  onClick={() => setEditingDetails(false)}
                  title={intl.formatMessage({
                    id: "actions.back",
                    defaultMessage: "Back",
                  })}
                >
                  <ChevronLeft size={18} />
                </Button>
                <h2 className="text-base font-semibold leading-tight truncate min-w-0">
                  {intl.formatMessage(
                    {
                      id: "actions.edit_entity",
                      defaultMessage: "Edit {entityType}",
                    },
                    {
                      entityType: intl
                        .formatMessage({
                          id: "scene",
                          defaultMessage: "Scene",
                        })
                        .toLocaleLowerCase(),
                    },
                  )}
                </h2>
              </div>
              {/* The form owns its own scroll body + anchored action
                  bar via flex-col layout, so we just give it the
                  remaining height of the parent. */}
              <div className="flex-1 min-h-0">
                <SceneEditForm
                  scene={scene}
                  onSaved={() => setEditingDetails(false)}
                />
              </div>
            </div>
          }
        />
      ),
    },
    {
      id: "markers",
      label: intl.formatMessage({ id: "markers", defaultMessage: "Markers" }),
      shortcut: "k",
      content: (
        <SceneMarkersTab
          scene={scene}
          onSeek={handleSeek}
          getCurrentTime={getPlayerPosition}
          onBoundsChange={handleBoundsChange}
          registerBoundSetter={registerBoundSetter}
        />
      ),
      selectable: false,
    },
    ...(scene.groups.length > 0
      ? [
          {
            id: "groups",
            label: intl.formatMessage({
              id: "groups",
              defaultMessage: "Groups",
            }),
            content: <SceneGroupsTab scene={scene} />,
            selectable: false,
          },
        ]
      : []),
    ...(scene.galleries.length > 0
      ? [
          {
            id: "galleries",
            label: intl.formatMessage({
              id: "galleries",
              defaultMessage: "Galleries",
            }),
            content: <SceneGalleriesTab scene={scene} />,
            selectable: false,
          },
        ]
      : []),
    {
      id: "fileinfo",
      label: intl.formatMessage({
        id: "file_info",
        defaultMessage: "File info",
      }),
      shortcut: "i",
      content: <SceneFileInfoTab scene={scene} />,
    },
    {
      id: "history",
      label: intl.formatMessage({ id: "history", defaultMessage: "History" }),
      shortcut: "h",
      content: <SceneHistoryTab scene={scene} />,
      selectable: false,
    },
    {
      id: "filters",
      label: intl.formatMessage({ id: "filters", defaultMessage: "Filters" }),
      content: <SceneVideoFilterTab sceneFile={scene.files[0]} />,
      selectable: false,
    },
  ];

  const player = (
    <ScenePlayer
      scene={scene}
      initialTimestamp={t}
      autoplay={autoplay}
      autostartEnabled={autostartEnabled}
      onEnded={handleOnEnded}
      sendSetTimestamp={sendSetTimestamp}
      sendGetCurrentTime={sendGetCurrentTime}
      enablePinchZoom={isDesktop}
      clipBoundsEdit={
        // Handles only render on desktop. On mobile / tablet portrait the
        // seek bar is narrow enough that drag handles are fiddly to grab,
        // and the editor's typed `mm:ss.fff` input + "use current time"
        // clock button cover the same ground without the precision
        // problem. Reuses the same breakpoint that drives the player /
        // tabs side-by-side layout.
        isDesktop && editBounds
          ? {
              start: editBounds.start,
              end: editBounds.end,
              onChange: handleClipBoundDrag,
            }
          : undefined
      }
    />
  );

  const toolbar = (
    <SceneToolbar
      scene={scene}
      onAddO={() => addO()}
      onAddPlay={() => addPlay()}
      onToggleOrganized={handleToggleOrganized}
      getPlayerPosition={getPlayerPosition}
      onDeleted={goBack}
      onScreenshotGenerated={async () => {
        await refetch();
      }}
    />
  );

  return (
    <MediaDetailLayout
      title={objectTitle(scene) || undefined}
      primaryContent={player}
      headerContent={toolbar}
      tabs={tabs}
      activeTab={activeTab}
      onTabChange={setActiveTab}
      onBack={goBack}
      mobilePageScroll
    />
  );
}

// ── Route ─────────────────────────────────────────────────────────────────────

export const Route = createFileRoute("/scenes/$sceneId")({
  validateSearch: zodValidator(searchSchema),
  component: SceneDetailPage,
});
