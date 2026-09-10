import type React from "react";
import { useCallback, useMemo, useRef, useState } from "react";
import { skipToken, useQuery, useMutation } from "@apollo/client/react";
import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { DropletsIcon, ExternalLinkIcon } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { objectTitle } from "src/core/files";
import { useIsTruncated } from "src/hooks/use-is-truncated";
import { Spinner } from "src/components/ui/spinner";
import { Badge } from "src/components/ui/badge";
import { Button } from "src/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "src/components/ui/tooltip";
import { cn } from "src/lib/utils";
import {
  LightboxOverlay,
  LightboxDate,
  LightboxDetails,
} from "./lightbox-overlay";
import { LightboxScenePlayer } from "./lightbox-scene-player";
import { PlayerCloseButton } from "@/components/player/player-close-button";
import type { SceneSlide, SceneSlideMarker } from "./scene-lightbox";
import { offlineEntryToSceneData } from "src/components/offline/offline-scene-adapter";
import type { OfflineEntry } from "src/components/offline/offline-db";
import { useOpfsBlobUrl } from "src/components/offline/use-opfs-blob";
import { useOfflineResumeWriter } from "src/components/offline/use-offline-resume-writer";

interface SceneSlideContentProps {
  slide: SceneSlide;
  /** True when this slide is the currently-viewed one (YARL offset === 0). */
  isActive: boolean;
  /** Toggles the lightbox-level fullscreen (YARL's Fullscreen plugin) so
   *  fullscreen captures the slide UI (title, swipe nav, toolbar) — not
   *  just the video element. Forwarded to the player as
   *  `onToggleFullscreenOverride`. */
  onToggleFullscreen: () => boolean | undefined;
  /** Loop preference owned by the lightbox across opening sessions. */
  loopEnabled: boolean;
  onLoopToggle: () => void;
  /** Advances to the next lightbox slide. Forwarded to the player as
   *  `onNext`, which both lights up the auto-advance toggle and
   *  triggers the swipe when the video ends with auto-advance on. */
  onNext: () => void;
  onClose?: () => void;
}

function PendingPlayerClose({ onClose }: { onClose?: () => void }) {
  return onClose ? (
    <div className="absolute inset-x-0 bottom-0 flex justify-end px-[max(0.375rem,env(safe-area-inset-left,0px),env(safe-area-inset-right,0px))] pb-[max(0.25rem,env(safe-area-inset-bottom,0px))]">
      <PlayerCloseButton onClose={onClose} />
    </div>
  ) : null;
}

export function SceneSlideContent(props: SceneSlideContentProps) {
  return props.isActive ? (
    <ActiveSceneSlide {...props} />
  ) : (
    <SceneSlidePoster slide={props.slide} />
  );
}

function SceneSlidePoster({ slide }: { slide: SceneSlide }) {
  return (
    <div className="absolute inset-0 flex items-center justify-center bg-black">
      {slide.posterSrc && (
        <img
          src={slide.posterSrc}
          alt={slide.title ?? ""}
          className="max-w-full max-h-full object-contain select-none"
          draggable={false}
        />
      )}
    </div>
  );
}

function computeMarkerEnd(
  scene: NonNullable<GQL.FindSceneQuery["findScene"]>,
  markerId: string,
  fallbackSeconds: number,
): number | undefined {
  const own = scene.scene_markers.find((m) => m.id === markerId);
  const start = own?.seconds ?? fallbackSeconds;
  if (own?.end_seconds != null) return own.end_seconds;
  // Implicit end: next marker on the same scene by start time, or scene
  // duration. Strict `>` skips coincident markers — they share a boundary
  // and would otherwise zero-length the range.
  const next = scene.scene_markers
    .filter((m) => m.seconds > start)
    .reduce<number | undefined>(
      (acc, m) => (acc == null || m.seconds < acc ? m.seconds : acc),
      undefined,
    );
  if (next != null) return next;
  const fileDuration = scene.files[0]?.duration;
  return fileDuration ?? undefined;
}

/**
 * Returns an `onClickCapture` handler that detects clicks on anchors with
 * `target="_blank"` inside the bound element and pauses playback via the
 * `pauseRef` (captured from the player's `sendPause` callback). Opening a
 * link in a new tab focuses that tab; continuing playback in the now-
 * background tab is confusing for the user.
 */
function useNewTabLinkPause(pauseRef: React.RefObject<(() => void) | null>) {
  return useCallback(
    (e: React.MouseEvent<HTMLElement>) => {
      const target = e.target;
      if (!(target instanceof Element)) return;
      const anchor = target.closest('a[target="_blank"]');
      if (anchor) pauseRef.current?.();
    },
    [pauseRef],
  );
}

function ActiveSceneSlide({
  slide,
  onToggleFullscreen,
  loopEnabled,
  onLoopToggle,
  onNext,
  onClose,
}: SceneSlideContentProps) {
  const { data, loading } = useQuery(
    GQL.FindSceneDocument,
    slide.loading || slide.offlineEntry
      ? skipToken
      : { variables: { id: slide.sceneId } },
  );
  const blob = useOpfsBlobUrl(slide.offlineEntry?.scene_id);
  const offlineScene = useMemo(
    () =>
      slide.offlineEntry && blob.url
        ? offlineEntryToSceneData(slide.offlineEntry, blob.url)
        : undefined,
    [slide.offlineEntry, blob.url],
  );
  // Apollo may retain the previous result while variables change. Never
  // expose that result as the newly selected scene.
  const scene = slide.loading
    ? undefined
    : slide.offlineEntry
      ? offlineScene
      : data?.findScene?.id === slide.sceneId
        ? data.findScene
        : undefined;

  // Keep the player mounted through query/OPFS gaps and boundary sentinels.
  // Suspended playback clears its source and releases the previous transcode;
  // the pending poster masks its retained layout until the new scene is ready.
  const [retainedScene, setRetainedScene] = useState(scene);
  if (scene && scene !== retainedScene) setRetainedScene(scene);
  const playerScene = scene ?? retainedScene;
  const pending = !scene;
  const [chromeVisible, setChromeVisible] = useState(true);
  const pauseRef = useRef<(() => void) | null>(null);
  const handleSlideClickCapture = useNewTabLinkPause(pauseRef);
  const { sendGetCurrentTime } = useOfflineResumeWriter(
    scene ? slide.offlineEntry?.scene_id : undefined,
    slide.offlineEntry?.last_position_seconds,
  );

  const marker = slide.marker;
  const clipRange = useMemo(() => {
    if (!scene || !marker) return undefined;
    const end = computeMarkerEnd(scene, marker.id, marker.seconds);
    return end != null && end > marker.seconds
      ? { start: marker.seconds, end }
      : undefined;
  }, [scene, marker]);
  const markerPosterSrc = marker
    ? (scene?.scene_markers.find((m) => m.id === marker.id)?.screenshot ??
      slide.posterSrc)
    : undefined;
  const playbackKey = JSON.stringify([
    slide.sceneId,
    marker?.id,
    Boolean(slide.offlineEntry),
  ]);

  return (
    <div
      className="relative size-full flex items-center justify-center bg-black"
      onClickCapture={handleSlideClickCapture}
    >
      {playerScene && (
        <LightboxScenePlayer
          scene={playerScene}
          playbackKey={playbackKey}
          suspended={pending}
          loopEnabled={loopEnabled}
          onLoopToggle={onLoopToggle}
          onToggleFullscreen={onToggleFullscreen}
          onControlsVisibilityChange={setChromeVisible}
          onNext={slide.offlineEntry ? undefined : onNext}
          onClose={onClose}
          initialTimestamp={
            slide.offlineEntry ? undefined : (marker?.seconds ?? 0)
          }
          clipRange={clipRange}
          posterSrc={markerPosterSrc}
          sendGetCurrentTime={sendGetCurrentTime}
          sendPause={(pause) => {
            pauseRef.current = pause;
          }}
          topOverlay={
            scene &&
            (slide.offlineEntry ? (
              <OfflineSceneOverlay
                entry={slide.offlineEntry}
                visible={chromeVisible}
              />
            ) : marker ? (
              <MarkerOverlay
                scene={scene}
                marker={marker}
                visible={chromeVisible}
              />
            ) : (
              <SceneOverlay scene={scene} visible={chromeVisible} />
            ))
          }
        />
      )}
      {pending && (
        <div className="absolute inset-0">
          <SceneSlidePoster slide={slide} />
          {(slide.loading ||
            (slide.offlineEntry ? !(blob.error || blob.missing) : loading)) && (
            <div className="absolute inset-0 flex items-center justify-center">
              <Spinner className="size-10 text-white/70" />
            </div>
          )}
          <PendingPlayerClose onClose={onClose} />
        </div>
      )}
    </div>
  );
}

// Offline overlay: scene title, date, and performer / studio badges from the
// IDB snapshot. Read-only — no mutating buttons.
function OfflineSceneOverlay({
  entry,
  visible,
}: {
  entry: OfflineEntry;
  visible: boolean;
}) {
  const title = entry.title?.trim();
  const performers = entry.performers ?? [];
  return (
    <LightboxOverlay
      position="top"
      passThrough
      gradientVisible={visible}
      className={cn(
        "transition-opacity duration-300",
        visible ? "opacity-100" : "opacity-0",
      )}
    >
      {title && (
        <div className="text-sm font-medium pointer-events-auto min-w-0 truncate">
          {title}
        </div>
      )}
      <LightboxDate date={entry.date} />
      {entry.studio_name && (
        <div className="text-xs text-white/75 pointer-events-auto min-w-0 truncate">
          {entry.studio_name}
        </div>
      )}
      {performers.length > 0 && (
        <div className="flex flex-wrap gap-1 pointer-events-auto">
          {performers.map((p) => (
            <Badge
              key={p.id}
              variant="secondary"
              className="bg-white/15 text-white border-0"
            >
              {p.name}
            </Badge>
          ))}
        </div>
      )}

      {entry.details && <LightboxDetails text={entry.details} />}
    </LightboxOverlay>
  );
}

function SceneTitleLink({
  sceneId,
  title,
  className,
}: {
  sceneId: string;
  title: string;
  className?: string;
}) {
  const [ref, truncated] = useIsTruncated<HTMLAnchorElement>();
  return (
    <Tooltip disabled={!truncated}>
      <TooltipTrigger
        render={
          <Link
            ref={ref}
            to="/scenes/$sceneId"
            params={{ sceneId }}
            target="_blank"
            rel="noreferrer"
            className={cn(
              "text-sm font-medium hover:underline truncate",
              className,
            )}
          >
            {title}
          </Link>
        }
      />
      <TooltipContent>{title}</TooltipContent>
    </Tooltip>
  );
}

// Title + date + performer badges over the top of the active slide. Hosted in
// `<LightboxOverlay position="top">`, which handles the visual shell
// (text-shadow, hover gradient, pointer-events gating).
function SceneOverlay({
  scene,
  visible,
}: {
  scene: NonNullable<GQL.FindSceneQuery["findScene"]>;
  visible: boolean;
}) {
  const intl = useIntl();
  const title = objectTitle(scene).trim();
  const performers = scene.performers ?? [];
  const oCounter = scene.o_counter ?? 0;

  const [addO] = useMutation(GQL.SceneAddODocument, {
    variables: { id: scene.id },
    update(cache, { data: result }) {
      if (!result?.sceneAddO) return;
      cache.modify({
        id: cache.identify({ __typename: "Scene", id: scene.id }),
        fields: {
          o_counter: () => result.sceneAddO.count,
          o_history: () => result.sceneAddO.history,
        },
      });
    },
  });

  return (
    <LightboxOverlay
      position="top"
      passThrough
      gradientVisible={visible}
      className={cn(
        "transition-opacity duration-300",
        visible ? "opacity-100" : "opacity-0",
      )}
    >
      {title && (
        <div className="flex items-center gap-1.5 pointer-events-auto min-w-0">
          <SceneTitleLink sceneId={scene.id} title={title} />
          <ExternalLinkIcon className="size-3 shrink-0 opacity-60" />
        </div>
      )}

      <LightboxDate date={scene.date} />

      {performers.length > 0 && (
        <div className="flex flex-wrap gap-1 pointer-events-auto">
          {performers.map((p) => (
            <Badge
              key={p.id}
              variant="secondary"
              className="bg-white/15 text-white border-0 hover:bg-white/25"
              render={
                <Link
                  to="/performers/$performerId"
                  params={{ performerId: p.id }}
                  target="_blank"
                  rel="noreferrer"
                />
              }
            >
              {p.name}
            </Badge>
          ))}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-4 pointer-events-auto">
        <Button
          variant="outline"
          className="h-auto bg-transparent px-2 py-1 text-[0.8125rem] gap-1 text-white/80 hover:text-white border-white/20 hover:bg-white/10"
          onClick={() => addO()}
          title={intl.formatMessage({
            id: "actions.increment_o",
            defaultMessage: "Add O",
          })}
        >
          <DropletsIcon size={14} />
          {oCounter}
        </Button>
      </div>

      {scene.details && <LightboxDetails text={scene.details} />}
    </LightboxOverlay>
  );
}

// Marker overlay for marker-mode slides. Stack from top: marker title
// (links to the scene's markers tab), scene title (links to the scene),
// scene date, performer badges, then primary-tag-first marker tags. All tag chips
// share the same style; the primary tag's only distinction is being
// listed first.
function MarkerOverlay({
  scene,
  marker,
  visible,
}: {
  scene: NonNullable<GQL.FindSceneQuery["findScene"]>;
  marker: SceneSlideMarker;
  visible: boolean;
}) {
  const sceneTitle = objectTitle(scene).trim();
  const performers = scene.performers ?? [];
  const otherTags = marker.tags.filter((t) => t.id !== marker.primaryTag.id);
  const orderedTags = [marker.primaryTag, ...otherTags];
  return (
    <LightboxOverlay
      position="top"
      passThrough
      gradientVisible={visible}
      className={cn(
        "transition-opacity duration-300",
        visible ? "opacity-100" : "opacity-0",
      )}
    >
      <div className="flex items-center gap-1.5 pointer-events-auto min-w-0">
        <MarkerTitleLink sceneId={scene.id} title={marker.title} />
        <ExternalLinkIcon className="size-3 shrink-0 opacity-60" />
      </div>

      {sceneTitle && (
        <div className="flex items-center gap-1.5 pointer-events-auto min-w-0">
          <SceneTitleLink
            sceneId={scene.id}
            title={sceneTitle}
            className="text-xs font-normal text-white/75 hover:text-white"
          />
          <ExternalLinkIcon className="size-3 shrink-0 opacity-60" />
        </div>
      )}

      <LightboxDate date={scene.date} />

      {performers.length > 0 && (
        <div className="flex flex-wrap gap-1 pointer-events-auto">
          {performers.map((p) => (
            <Badge
              key={p.id}
              variant="secondary"
              className="bg-white/15 text-white border-0 hover:bg-white/25"
              render={
                <Link
                  to="/performers/$performerId"
                  params={{ performerId: p.id }}
                  target="_blank"
                  rel="noreferrer"
                />
              }
            >
              {p.name}
            </Badge>
          ))}
        </div>
      )}

      <div className="flex flex-wrap gap-1 pointer-events-auto">
        {orderedTags.map((t) => (
          <Badge
            key={t.id}
            variant="secondary"
            className="bg-white/15 text-white border-0 hover:bg-white/25"
            render={
              <Link
                to="/tags/$tagId"
                params={{ tagId: t.id }}
                target="_blank"
                rel="noreferrer"
              />
            }
          >
            {t.name}
          </Badge>
        ))}
      </div>
    </LightboxOverlay>
  );
}

// Marker title links to the scene's markers tab — the user came in via
// the marker lightbox, so jumping to the markers list is more useful
// than dropping them on the scene's main detail tab.
function MarkerTitleLink({
  sceneId,
  title,
}: {
  sceneId: string;
  title: string;
}) {
  const [ref, truncated] = useIsTruncated<HTMLAnchorElement>();
  return (
    <Tooltip disabled={!truncated}>
      <TooltipTrigger
        render={
          <Link
            ref={ref}
            to="/scenes/$sceneId"
            params={{ sceneId }}
            search={{ tab: "markers" }}
            target="_blank"
            rel="noreferrer"
            className="text-sm font-medium hover:underline truncate"
          >
            {title}
          </Link>
        }
      />
      <TooltipContent>{title}</TooltipContent>
    </Tooltip>
  );
}
