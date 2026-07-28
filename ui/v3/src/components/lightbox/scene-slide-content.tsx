import type React from "react";
import { useCallback, useRef, useState } from "react";
import { useQuery, useMutation } from "@apollo/client/react";
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
  /** Lightbox-scoped loop preference. Controls the player's loop toggle
   *  externally so the value survives the per-scene player remount on
   *  slide swipe. */
  loopEnabled: boolean;
  onLoopToggle: () => void;
  /** Advances to the next lightbox slide. Forwarded to the player as
   *  `onNext`, which both lights up the auto-advance toggle and
   *  triggers the swipe when the video ends with auto-advance on. */
  onNext: () => void;
}

export function SceneSlideContent({
  slide,
  isActive,
  onToggleFullscreen,
  loopEnabled,
  onLoopToggle,
  onNext,
}: SceneSlideContentProps) {
  // Non-active and sentinel slides render a cheap poster only.
  if (!isActive || slide.loading) {
    return (
      <div className="w-full h-full flex items-center justify-center bg-black">
        {slide.posterSrc && (
          <img
            src={slide.posterSrc}
            alt={slide.title ?? ""}
            className="max-w-full max-h-full object-contain select-none"
            draggable={false}
          />
        )}
        {slide.loading && (
          <div className="absolute inset-0 flex items-center justify-center">
            <Spinner className="size-10 text-white/70" />
          </div>
        )}
      </div>
    );
  }

  // `key` forces a full remount on scene change so Video.js tears down cleanly
  // instead of swapping its source in-place mid-animation (which can strand the
  // media in a buffered-but-paused state). Markers on the same scene also
  // remount so the new `initialTimestamp` takes effect on the fresh player.
  if (slide.offlineEntry) {
    return (
      <ActiveOfflineSceneSlide
        key={slide.sceneId}
        slide={slide}
        offlineEntry={slide.offlineEntry}
        onToggleFullscreen={onToggleFullscreen}
        loopEnabled={loopEnabled}
        onLoopToggle={onLoopToggle}
      />
    );
  }
  return (
    <ActiveSceneSlide
      key={slide.marker ? `${slide.sceneId}:${slide.marker.id}` : slide.sceneId}
      slide={slide}
      onToggleFullscreen={onToggleFullscreen}
      loopEnabled={loopEnabled}
      onLoopToggle={onLoopToggle}
      onNext={onNext}
    />
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
      const target = e.target as Element | null;
      if (!target) return;
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
}: {
  slide: SceneSlide;
  onToggleFullscreen: () => boolean | undefined;
  loopEnabled: boolean;
  onLoopToggle: () => void;
  onNext: () => void;
}) {
  const { data, loading } = useQuery(GQL.FindSceneDocument, {
    variables: { id: slide.sceneId },
  });

  const scene = data?.findScene;

  // Mirrors the player's controls bar fade so the title / performer
  // overlay disappears together with the bottom controls when the user
  // is idle, then reappears on the next user-active gesture.
  const [chromeVisible, setChromeVisible] = useState(true);

  // The marker lightbox presents each marker as a standalone clip — the
  // player's timeline, time display, and skip-step bounds all use the
  // marker range as if it were the whole video, with loop / auto-advance
  // firing at the clip boundary. See ScenePlayer's `clipRange` prop.
  const clipRange = (() => {
    if (!scene || !slide.marker) return undefined;
    const end = computeMarkerEnd(scene, slide.marker.id, slide.marker.seconds);
    if (end == null || end <= slide.marker.seconds) return undefined;
    return { start: slide.marker.seconds, end };
  })();

  // Pre-play poster: marker slides use the marker's own screenshot so the
  // brief frame shown before playback starts already matches the clip,
  // not the scene's cover frame from elsewhere in the video. Falls back
  // to `slide.posterSrc` (also the marker screenshot, captured when the
  // slide was built) so a missing/late `scene_markers` entry can't drop
  // us onto the scene cover via `<ScenePlayer>`'s default fallback.
  const markerPosterSrc = slide.marker
    ? (scene?.scene_markers.find((m) => m.id === slide.marker!.id)
        ?.screenshot ?? slide.posterSrc)
    : undefined;

  // Pauser captured from the player via `sendPause`; invoked on
  // `target="_blank"` link clicks within the slide so the video doesn't
  // keep playing in a backgrounded tab.
  const pauseRef = useRef<(() => void) | null>(null);
  const handleSlideClickCapture = useNewTabLinkPause(pauseRef);

  return (
    <div
      className="relative w-full h-full flex items-center justify-center bg-black"
      onClickCapture={handleSlideClickCapture}
    >
      {scene ? (
        <LightboxScenePlayer
          scene={scene}
          loopEnabled={loopEnabled}
          onLoopToggle={onLoopToggle}
          onToggleFullscreen={onToggleFullscreen}
          onControlsVisibilityChange={setChromeVisible}
          onNext={onNext}
          initialTimestamp={slide.marker?.seconds ?? 0}
          clipRange={clipRange}
          posterSrc={markerPosterSrc}
          sendPause={(p) => {
            pauseRef.current = p;
          }}
          topOverlay={
            slide.marker ? (
              <MarkerOverlay
                scene={scene}
                marker={slide.marker}
                visible={chromeVisible}
              />
            ) : (
              <SceneOverlay scene={scene} visible={chromeVisible} />
            )
          }
        />
      ) : (
        <>
          {slide.posterSrc && (
            <img
              src={slide.posterSrc}
              alt={slide.title ?? ""}
              className="max-w-full max-h-full object-contain select-none"
              draggable={false}
            />
          )}
          {loading && (
            <div className="absolute inset-0 flex items-center justify-center">
              <Spinner className="size-10 text-white/70" />
            </div>
          )}
        </>
      )}
    </div>
  );
}

// Offline-mode active slide. Resolves the OPFS file → blob URL on
// mount, builds a fake scene from the IDB-snapshotted `OfflineEntry`,
// and feeds both to the same `<ScenePlayer>` the online slide uses.
//
// Differences from the online active slide:
//   - No GraphQL `findScene` round-trip — the snapshot has the
//     player-essential fields and the rest doesn't matter for offline.
//   - Mutating overlay buttons (Add O, etc.) are suppressed because the
//     scene id may not exist server-side, and even when it does we
//     intentionally don't write to the live record from the offline
//     surface.
//   - No `onNext` wiring: the offline carousel doesn't have an
//     auto-advance use case (the user explicitly opens what they
//     downloaded), and the slide-swipe still works via the lightbox's
//     own controls.
//   - Resume position writes back to IDB on a 5 s poll + unmount, same
//     pattern as `routes/offline/$sceneId.tsx`.
function ActiveOfflineSceneSlide({
  slide,
  offlineEntry,
  onToggleFullscreen,
  loopEnabled,
  onLoopToggle,
}: {
  slide: SceneSlide;
  offlineEntry: OfflineEntry;
  onToggleFullscreen: () => boolean | undefined;
  loopEnabled: boolean;
  onLoopToggle: () => void;
}) {
  const blob = useOpfsBlobUrl(offlineEntry.scene_id);
  const { sendGetCurrentTime } = useOfflineResumeWriter(
    offlineEntry.scene_id,
    offlineEntry.last_position_seconds,
  );
  const [chromeVisible, setChromeVisible] = useState(true);

  const fakeScene = blob.url
    ? offlineEntryToSceneData(offlineEntry, blob.url)
    : null;

  return (
    <div className="relative w-full h-full flex items-center justify-center bg-black">
      {fakeScene ? (
        <LightboxScenePlayer
          scene={fakeScene}
          loopEnabled={loopEnabled}
          onLoopToggle={onLoopToggle}
          onToggleFullscreen={onToggleFullscreen}
          onControlsVisibilityChange={setChromeVisible}
          sendGetCurrentTime={sendGetCurrentTime}
          topOverlay={
            <OfflineSceneOverlay entry={offlineEntry} visible={chromeVisible} />
          }
        />
      ) : (
        <>
          {slide.posterSrc && (
            <img
              src={slide.posterSrc}
              alt={slide.title ?? ""}
              className="max-w-full max-h-full object-contain select-none"
              draggable={false}
            />
          )}
          {!(blob.error || blob.missing) && (
            <div className="absolute inset-0 flex items-center justify-center">
              <Spinner className="size-10 text-white/70" />
            </div>
          )}
        </>
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
