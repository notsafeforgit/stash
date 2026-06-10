import type React from "react";
import { useCallback } from "react";
import { useApolloClient } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { objectTitle } from "src/core/files";
import { EntityCard } from "./entity-card";
import { useMobileGridCols } from "src/components/list/mobile-grid-context";
import { useCardLayout } from "src/components/list/card-layout-context";
import { useZoomIndex } from "src/components/list/zoom-index-context";
import { useCardAspect } from "src/components/list/card-aspect-context";
import { cn } from "src/lib/utils";
import { useSceneContextMenu } from "./use-scene-context-menu";

// SceneCardScene: union of mobile + slim fields.
// Base (always present): MobileSceneDataFragment fields.
// Optional desktop fields come from SlimSceneDataFragment.
export type SceneCardScene = Omit<
  GQL.MobileSceneDataFragment,
  "paths" | "files"
> & {
  paths: {
    screenshot?: string | null;
    preview?: string | null;
    webp?: string | null;
    vtt?: string | null;
    interactive_heatmap?: string | null;
  };
  files: Array<{
    id: string;
    path: string;
    duration: number;
    width: number | null;
    height: number | null;
    size?: number;
    video_codec?: string | null;
    audio_codec?: string | null;
    fingerprints?: Array<{ type: string; value: string }>;
  }>;
  interactive?: boolean | null;
  interactive_speed?: number | null;
  o_counter?: number | null;
  organized?: boolean;
  tags?: GQL.SlimSceneDataFragment["tags"];
  performers?: GQL.SlimSceneDataFragment["performers"];
  groups?: GQL.SlimSceneDataFragment["groups"];
  scene_markers?: GQL.SlimSceneDataFragment["scene_markers"];
  galleries?: GQL.SlimSceneDataFragment["galleries"];
};

interface SceneCardProps {
  scene: SceneCardScene;
  isMobile?: boolean;
  selected?: boolean;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  onPreviewClick?: () => void;
  onEdit?: () => void;
  zoomIndex?: number;
  hidePerformers?: boolean;
  /** Link target. Defaults to `/scenes/{id}`. Override for views that
   *  shouldn't navigate to the regular detail page (e.g. the Offline
   *  view, which links to its own player route). */
  href?: string;
  /** Replace the default context menu entirely. Used by the Offline
   *  view to surface "Save to Files" / "Re-download" / "Delete"
   *  instead of the regular Edit / Generate / Merge / Delete set. */
  contextMenu?: React.ReactNode;
  /** Forwarded to EntityCard so callers passing a `contextMenu`
   *  override (e.g. the Offline view) can wire `useBulkCardActions`
   *  and lazily compute selection-dependent menu items at open time. */
  onContextMenuOpen?: () => void;
}

// ── Scene wall overlay ────────────────────────────────────────────────────────

function formatWallPerformers(
  performers: Array<{ name: string }> | null | undefined,
): string {
  if (!performers || performers.length === 0) return "";
  const names = performers.map((p) => p.name);
  if (names.length === 1) return names[0];
  // Join last two with " & ", rest with ", "
  const head = names.slice(0, -2);
  const tail = names.slice(-2).join(" & ");
  return [...head, tail].join(", ");
}

interface SceneWallOverlayProps {
  title: string;
  performers?: Array<{ name: string }> | null;
  date?: string | null;
}

function SceneWallOverlay({ title, performers, date }: SceneWallOverlayProps) {
  const performersStr = formatWallPerformers(performers);
  if (!title && !performersStr && !date) return null;
  return (
    <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/80 via-black/40 to-transparent pt-6 pb-1.5 px-2 pointer-events-none">
      {title && (
        <p className="text-white text-[0.7rem] font-semibold leading-tight truncate">
          {title}
        </p>
      )}
      {performersStr && (
        <p
          className={cn(
            "text-white/80 text-[0.65rem] leading-snug",
            title && "mt-0.5",
          )}
        >
          {performersStr}
        </p>
      )}
      {date && (
        <p className="text-white/70 text-[0.6rem] leading-tight mt-0.5">
          {date}
        </p>
      )}
    </div>
  );
}

export const SceneCard: React.FC<SceneCardProps> = ({
  scene,
  isMobile = false,
  selected,
  onSelectedChanged,
  onPreviewClick,
  onEdit,
  hidePerformers = false,
  href,
  contextMenu: contextMenuOverride,
  onContextMenuOpen: onContextMenuOpenOverride,
}) => {
  const file = scene.files[0];
  const resolution =
    file?.width && file?.height
      ? { width: file.width, height: file.height }
      : null;

  const cardAspect = useCardAspect();
  const fileIsPortrait = file ? (file.height ?? 0) > (file.width ?? 0) : false;
  const isPortrait =
    cardAspect === "portrait"
      ? true
      : cardAspect === "landscape"
        ? false
        : fileIsPortrait;
  const mobileGridCols = useMobileGridCols();
  const cardLayout = useCardLayout();
  const zoomIndex = useZoomIndex();
  const showDetails =
    cardLayout === "details" ||
    (isMobile && mobileGridCols === 1) ||
    (!isMobile && cardLayout === "grid" && zoomIndex <= 1);
  const showExtras = showDetails || !isMobile;

  const { menuContent, dialogs, onContextMenuOpen } = useSceneContextMenu({
    scene,
    onSelectedChanged,
    onEdit,
  });

  const contextMenu = contextMenuOverride ?? menuContent;
  // When the caller supplies their own context menu, they own the
  // open-time hook too (e.g. the Offline view's bulk hook). Otherwise
  // fall back to the default-menu's selection-snapshot hook.
  const effectiveOnContextMenuOpen =
    onContextMenuOpenOverride ?? onContextMenuOpen;

  // Skip prefetch when href is overridden — the override may target a
  // non-detail route (e.g. the Offline view's player), so warming
  // FindSceneDocument would be wasted (and may fail offline).
  const client = useApolloClient();
  const prefetch = useCallback(() => {
    void client.query({
      query: GQL.FindSceneDocument,
      variables: { id: scene.id },
      fetchPolicy: "cache-first",
    });
  }, [client, scene.id]);

  return (
    <>
      <EntityCard
        id={scene.id}
        href={href ?? `/scenes/${scene.id}`}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        onPreviewClick={onPreviewClick}
        contextMenu={contextMenu}
        onContextMenuOpen={effectiveOnContextMenuOpen}
        prefetch={href ? undefined : prefetch}
        className="scene-card"
      >
        <EntityCard.SelectCheckbox />
        <EntityCard.Preview
          image={scene.paths.screenshot}
          video={scene.paths.preview}
          animated={scene.paths.webp}
          vtt={scene.paths.vtt}
          studioImagePath={scene.studio?.image_path}
          ratingBanner={scene.rating100}
          resumeTime={scene.resume_time}
          isPortrait={isPortrait}
          naturalIsPortrait={file ? fileIsPortrait : undefined}
          duration={file?.duration}
          resolution={resolution}
          organized={scene.organized}
          oCounter={scene.o_counter}
        >
          {cardLayout === "wall" && (
            <SceneWallOverlay
              title={objectTitle(scene)}
              performers={hidePerformers ? undefined : scene.performers}
              date={scene.date}
            />
          )}
        </EntityCard.Preview>
        <EntityCard.Body>
          <EntityCard.Title>{objectTitle(scene)}</EntityCard.Title>
          {showDetails && scene.details && (
            <EntityCard.Subtitle>{scene.details}</EntityCard.Subtitle>
          )}
          {(scene.date || scene.studio) && (
            <EntityCard.Subtitle noTooltip>
              {[scene.date, scene.studio?.name].filter(Boolean).join(" · ")}
            </EntityCard.Subtitle>
          )}
          {showExtras && !hidePerformers && (
            <EntityCard.Performers performers={scene.performers} />
          )}
          {showExtras && <EntityCard.Tags tags={scene.tags} />}
          <EntityCard.Rating rating100={scene.rating100} />
        </EntityCard.Body>
      </EntityCard>
      {dialogs}
    </>
  );
};
