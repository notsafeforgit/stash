import type React from "react";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { cn } from "src/lib/utils";
import { Check, Droplets } from "lucide-react";
import { HoverScrubber } from "./hover-scrubber";
import { useSpriteInfo } from "src/hooks/use-sprite-info";
import { useIsTruncated } from "src/hooks/use-is-truncated";
import { useCardAspect } from "src/components/list/card-aspect-context";
import { useCardLayout } from "src/components/list/card-layout-context";
import { useMobileGridCols } from "src/components/list/mobile-grid-context";
import { useIsTouch } from "src/utils/screen";
import { useConfigurationContextOptional } from "src/hooks/config";
import { PreviewDefaultType } from "src/core/generated-graphql";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "src/components/ui/tooltip";
import {
  ContextMenu,
  ContextMenuTrigger,
} from "src/components/ui/context-menu";

// ── Context ───────────────────────────────────────────────────────────────────

interface EntityCardCtx {
  id: string;
  isHovered: boolean;
  /**
   * Viewport-width-based "narrow screen" flag. Use for layout/density
   * decisions (whether to show studio logo, rating ribbon, single-column
   * mode, etc.). Do NOT use for hover-gated UI — see `isTouch`.
   */
  isMobile: boolean;
  /**
   * Primary-pointer-is-coarse flag (`(pointer: coarse)` media query). Use
   * for any UI that only makes sense with hover: video preview elements,
   * sprite scrubber, `onMouseEnter`/`onMouseLeave` hooks. A 12" iPad has
   * `isMobile: false` but `isTouch: true` — gating those on `isMobile`
   * would mount video previews that no input can ever trigger.
   */
  isTouch: boolean;
  isDetails: boolean;
  isWall: boolean;
  selected?: boolean;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  onPreviewClick?: () => void;
}

const EntityCardCtx = createContext<EntityCardCtx>({
  id: "",
  isHovered: false,
  isMobile: false,
  isTouch: false,
  isDetails: false,
  isWall: false,
});

// ── Root ─────────────────────────────────────────────────────────────────────

interface EntityCardRootProps {
  id: string;
  href: string;
  isMobile?: boolean;
  selected?: boolean;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  /** Called when the preview area is tapped/clicked instead of navigating. */
  onPreviewClick?: () => void;
  /** Context menu content to show on right-click / long-press. */
  contextMenu?: React.ReactNode;
  /** Called when the context menu opens — use to lazily compute selection-dependent menu items. */
  onContextMenuOpen?: () => void;
  /**
   * Warm the Apollo cache for the destination route. Fires once on first
   * pointer-enter (hover on desktop, finger-down on touch). The destination
   * page's `useQuery` then resolves synchronously from cache, eliminating
   * the loading-skeleton flash on the common path.
   */
  prefetch?: () => void;
  className?: string;
  children: React.ReactNode;
}

function EntityCardRoot({
  id,
  href,
  isMobile = false,
  selected,
  onSelectedChanged,
  onPreviewClick,
  contextMenu,
  onContextMenuOpen,
  prefetch,
  className,
  children,
}: EntityCardRootProps) {
  const [isHovered, setIsHovered] = useState(false);
  const cardLayout = useCardLayout();
  const isDetails = cardLayout === "details";
  const isWall = cardLayout === "wall";
  const isTouch = useIsTouch();
  const navigate = useNavigate();

  // Prefetch fires at most once per card mount: `pointerenter` covers both
  // desktop hover and the finger-down phase of a tap, so by the time the
  // click event lands, the Apollo cache is already warm.
  const prefetchedRef = useRef(false);
  function handlePointerEnter() {
    if (prefetchedRef.current) return;
    prefetchedRef.current = true;
    prefetch?.();
  }

  function doNavigate() {
    // Capture returnTo from window.location at click time — always synchronously
    // up-to-date, unlike useLocation() which lags behind router.history.replace().
    const returnTo =
      window.location.pathname + window.location.search + window.location.hash;
    // href is a dynamic runtime string — cast needed because TanStack Router's
    // `to` is typed as a union of registered route paths, not plain string.
    // viewTransition: the browser snapshots the current frame and cross-fades
    // to the new one, hiding React's reconcile cost behind a paint.
    navigate({
      to: href as never,
      state: { returnTo },
      viewTransition: true,
    });
  }

  // Mouse clicks on the article body — skip if the click originated from a
  // chip/button inside the card (they handle their own navigation).
  function handleArticleClick(e: React.MouseEvent<HTMLElement>) {
    // data-card-link marks the stretched anchor itself — don't skip it
    const chip = (e.target as HTMLElement).closest(
      "a[href]:not([data-card-link]), button",
    );
    if (chip) return;

    // Read selecting state from the DOM — avoids re-rendering all cards on
    // selecting state change; the [data-selecting] attribute on the grid
    // container is synced imperatively by EntityListPage.
    const articleEl = e.currentTarget as HTMLElement;
    const isSelecting = !!articleEl.closest("[data-selecting]");

    if (e.metaKey || e.ctrlKey) {
      // Read current selection from DOM so wall-mode cards (whose React selected
      // prop may be stale) toggle correctly without a re-render.
      onSelectedChanged?.(!(articleEl.dataset.selected === "true"), e.shiftKey);
      return;
    }
    if (isSelecting) {
      onSelectedChanged?.(!(articleEl.dataset.selected === "true"), e.shiftKey);
      return;
    }
    if (onPreviewClick) {
      const isPreview =
        (e.target as HTMLElement).closest("[data-entity-card-preview]") !==
        null;
      if (isPreview) {
        onPreviewClick();
        return;
      }
    }
    doNavigate();
  }

  // Keyboard activation of the stretched anchor (Tab + Enter). stopPropagation
  // prevents the event from also triggering handleArticleClick.
  function handleAnchorClick(e: React.MouseEvent<HTMLAnchorElement>) {
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey) return; // let browser open new tab
    e.preventDefault();
    const isSelecting = !!(e.currentTarget as HTMLElement).closest(
      "[data-selecting]",
    );
    if (isSelecting) {
      const article = (e.currentTarget as HTMLElement).closest<HTMLElement>(
        "article[data-id]",
      );
      onSelectedChanged?.(!(article?.dataset.selected === "true"), e.shiftKey);
      return;
    }
    doNavigate();
  }

  const article = (
    <article
      className={cn(
        "entity-card relative flex flex-col overflow-hidden bg-card text-sm text-card-foreground transition-shadow cursor-pointer h-full",
        isWall
          ? "rounded-none ring-0"
          : cn(
              "rounded-xl ring-1",
              selected
                ? "ring-2 ring-primary"
                : "ring-foreground/10 hover:shadow-md hover:ring-foreground/20",
            ),
        { mobile: isMobile },
        contextMenu && "select-none",
        className,
      )}
      data-id={id}
      data-layout={isWall ? "wall" : undefined}
      data-selected={selected ? "true" : undefined}
      onPointerEnter={prefetch ? handlePointerEnter : undefined}
      onMouseEnter={isTouch ? undefined : () => setIsHovered(true)}
      onMouseLeave={isTouch ? undefined : () => setIsHovered(false)}
      onClick={handleArticleClick}
    >
      {/*
        Stretched anchor sits behind the content (z-0) for keyboard nav
        and right-click / middle-click "open in new tab". Mouse clicks on
        the card body are handled by handleArticleClick on the article instead
        (because the content wrapper at z-[1] intercepts them before the anchor).
        Using a plain <a> avoids nesting <a> inside <a> — performer/tag Link
        chips inside the body are siblings in the DOM, not descendants of this anchor.
      */}
      {/*
        Stretched anchor sits behind the content (z-0) for keyboard nav
        and right-click / middle-click "open in new tab". Mouse clicks on
        the card body are handled by handleArticleClick on the article instead
        (because the content wrapper at z-[1] intercepts them before the anchor).
        `to` cast to `never` because href is a dynamic string; TanStack Router's
        to-prop is a registered-route union, not a plain string.
      */}
      <Link
        data-card-link
        className="absolute inset-0 z-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1"
        to={href as never}
        onClick={handleAnchorClick}
        viewTransition
        tabIndex={0}
        aria-label={href}
      />
      {/* Content wrapper — z-[1] so performer/tag chips are above the stretched link */}
      <div
        className={cn(
          "relative z-[1] flex flex-1",
          isDetails ? "flex-row" : "flex-col",
        )}
      >
        {children}
      </div>
    </article>
  );

  return (
    <EntityCardCtx.Provider
      value={{
        id,
        isHovered,
        isMobile,
        isTouch,
        isDetails,
        isWall,
        selected,
        onSelectedChanged,
        onPreviewClick,
      }}
    >
      {contextMenu ? (
        <ContextMenu
          onOpenChange={(open) => {
            if (open) onContextMenuOpen?.();
          }}
        >
          <ContextMenuTrigger className="h-full">{article}</ContextMenuTrigger>
          {contextMenu}
        </ContextMenu>
      ) : (
        article
      )}
    </EntityCardCtx.Provider>
  );
}

// ── Preview ───────────────────────────────────────────────────────────────────

interface PreviewBadges {
  duration?: number | null;
  resolution?: { width: number; height: number } | null;
  organized?: boolean;
  /** O-count to display as a non-interactive badge on the preview.
   *  Hidden when null / 0. The interactive +1 control lives elsewhere
   *  (scene detail toolbar, lightbox overlay) — the card is read-only. */
  oCounter?: number | null;
}

interface EntityCardPreviewProps extends PreviewBadges {
  image?: string | null;
  video?: string | null;
  /** Animated WebP preview URL — shown as the idle state when the
   *  `previewDefault` config is "animated". Skipped when null. */
  animated?: string | null;
  /** WebVTT sprite sheet URL — enables hover-scrubber on desktop */
  vtt?: string | null;
  /** Studio thumbnail URL — shown as small overlay badge */
  studioImagePath?: string | null;
  /** Rating 0-100 — shown as diagonal corner ribbon on desktop */
  ratingBanner?: number | null;
  /** Playback resume time in seconds — shown as progress bar */
  resumeTime?: number | null;
  isPortrait?: boolean;
  /** Pre-computed natural aspect from file dimensions — skips onLoad measurement and eliminates the crop→pillarbox flash */
  naturalIsPortrait?: boolean;
  children?: React.ReactNode;
}

function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (h > 0) {
    return `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
  }
  return `${m}:${String(s).padStart(2, "0")}`;
}

function resolutionLabel(width: number, height: number): string {
  const h = Math.min(width, height);
  if (h >= 2160) return "4K";
  if (h >= 1440) return "1440p";
  if (h >= 1080) return "1080p";
  if (h >= 720) return "720p";
  if (h >= 480) return "480p";
  return `${h}p`;
}

// Small translucent pill badge used for resolution/duration overlays
const badgeCls =
  "rounded bg-black/70 px-1.5 py-0.5 text-[0.65rem] font-semibold leading-none text-white shadow-sm backdrop-blur-[2px]";

// ── FadeInImage ───────────────────────────────────────────────────────────────
// Card preview thumbnails fade from opacity-0 to opacity-100 once the image
// finishes decoding. The browser still streams completions one-by-one as
// they finish; the fade smooths the per-image pop so the overall page reads
// as a coordinated reveal rather than a top-down cascade.
//
// Cache hits skip the fade via a module-scoped Set of URLs we've successfully
// loaded this session. On mount, if `src` is in the set we initialise
// `loaded=true` and render straight at opacity-100 — no fade. This matters
// for card remounts: the virtualiser reshuffles items into different rows
// when lanes change (e.g. a zoom-level switch), so cards unmount/remount
// and a per-instance "first paint" check would replay the fade on every
// image even though the bytes are already in the browser cache. Tracking
// "have we ever finished this URL" at module scope avoids that.
//
// `loading="lazy"` is intentionally omitted: native lazy-loading defers
// fetches as cards scroll into view, producing a top-down stagger that
// breaks the "all-at-once" feel. Eager fetching pushes all in-viewport
// requests to the network at mount; the browser's per-origin connection
// cap (~6) handles queueing, and the fade-in masks finish-time variance.

const fadeInLoadedSrcs = new Set<string>();

function FadeInImage({
  className,
  onLoad,
  src,
  ...rest
}: React.ImgHTMLAttributes<HTMLImageElement>) {
  const [loaded, setLoaded] = useState(() =>
    src ? fadeInLoadedSrcs.has(src) : false,
  );
  const ref = useRef<HTMLImageElement>(null);

  useLayoutEffect(() => {
    if (src && fadeInLoadedSrcs.has(src)) {
      setLoaded(true);
      return;
    }
    if (ref.current?.complete && ref.current.naturalWidth > 0) {
      if (src) fadeInLoadedSrcs.add(src);
      setLoaded(true);
    } else {
      setLoaded(false);
    }
  }, [src]);

  return (
    <img
      ref={ref}
      src={src}
      className={cn(
        "transition-opacity duration-200 ease-out",
        loaded ? "opacity-100" : "opacity-0",
        className,
      )}
      onLoad={(e) => {
        if (src) fadeInLoadedSrcs.add(src);
        setLoaded(true);
        onLoad?.(e);
      }}
      {...rest}
    />
  );
}

function EntityCardPreview({
  image,
  video,
  animated,
  vtt,
  studioImagePath,
  ratingBanner,
  resumeTime,
  isPortrait,
  naturalIsPortrait: naturalIsPortraitProp,
  duration,
  resolution,
  organized,
  oCounter,
  children,
}: EntityCardPreviewProps) {
  // Card preview behaviour is driven by two interface settings:
  //   - previewDefault: which asset to show when the card is idle
  //   - playVideoOnHover: whether hover swaps to the video preview
  // Both default to v2.5's effective behaviour (autoplay video, hover too).
  // Each idle mode falls back to the next-best asset if its preferred
  // source is missing: video → animated → image. We only ever mount media
  // elements whose URLs exist, so unavailable assets cost zero requests.
  const cfg = useConfigurationContextOptional()?.configuration.interface;
  const previewDefault = cfg?.previewDefault ?? PreviewDefaultType.Video;
  const playVideoOnHoverCfg = cfg?.playVideoOnHover ?? true;

  let idleMode: "image" | "animated" | "video";
  if (previewDefault === PreviewDefaultType.Video && video) {
    idleMode = "video";
  } else if (
    (previewDefault === PreviewDefaultType.Animated ||
      previewDefault === PreviewDefaultType.Video) &&
    animated
  ) {
    idleMode = "animated";
  } else {
    idleMode = "image";
  }
  // Hover-swap only makes sense when the idle state isn't already the
  // video. Touch devices don't get hover at all, so the mount is gated
  // again at the render site by `!isTouch`.
  const hoverVideoEnabled =
    playVideoOnHoverCfg && idleMode !== "video" && !!video;
  // The autoplay (idle === "video") path also needs the <video> element.
  const wantsVideoEl = idleMode === "video" || hoverVideoEnabled;
  const { isHovered, isMobile, isTouch, isDetails, isWall, onPreviewClick } =
    useContext(EntityCardCtx);
  const videoRef = useRef<HTMLVideoElement>(null);

  // VTT sprite scrubber: hover-only UI, gated on pointer type — touch
  // devices don't get hover, so don't bother fetching the sprite sheet.
  const sprites = useSpriteInfo(!isTouch ? (vtt ?? undefined) : undefined);
  const [scrubIndex, setScrubIndex] = useState<number | null>(null);

  const handleScrubIndex = useCallback(
    (index: number) => setScrubIndex(index),
    [],
  );

  // Clear scrub state when no longer hovering
  useEffect(() => {
    if (!isHovered) setScrubIndex(null);
  }, [isHovered]);

  // Drive the video element's play/pause state. When the idle preview is
  // already the video (autoplay mode) we keep it playing regardless of
  // hover; otherwise we only play on hover. The element only mounts on
  // non-touch devices (see the `!isTouch` guard at the render site), so
  // this effect is a no-op when the ref is unset on touch.
  useEffect(() => {
    const el = videoRef.current;
    if (!el) return;
    const shouldPlay = idleMode === "video" || isHovered;
    if (shouldPlay) {
      el.play().catch(() => {});
    } else {
      el.pause();
      el.currentTime = 0;
    }
  }, [isHovered, idleMode]);

  const currentSprite =
    sprites && scrubIndex != null ? sprites[scrubIndex] : null;

  // Track the natural aspect ratio of the loaded image so we only apply
  // blur pillarbox/letterbox when the image actually mismatches the forced aspect.
  // When naturalIsPortraitProp is provided (e.g. from file dimensions), seed the
  // state from it immediately — no onLoad measurement needed, no flash.
  const [naturalIsPortrait, setNaturalIsPortrait] = useState<boolean | null>(
    naturalIsPortraitProp ?? null,
  );
  const prevImageRef = useRef<string | null | undefined>(null);
  if (image !== prevImageRef.current) {
    prevImageRef.current = image;
    setNaturalIsPortrait(naturalIsPortraitProp ?? null);
  }
  const cardAspect = useCardAspect();
  const hasMismatch =
    !isDetails &&
    cardAspect !== "auto" &&
    naturalIsPortrait !== null &&
    naturalIsPortrait !== isPortrait;

  const resumePercent =
    resumeTime != null && duration != null && duration > 0
      ? Math.min(100, (resumeTime / duration) * 100)
      : null;

  // Wall mode: image fills the height determined by react-photo-album's row layout.
  // The parent wrapper div has an explicit pixel height set by PhotoAlbumWall's render.photo.
  // children renders as an absolute overlay (e.g. gradient footer).
  if (isWall && (image || video || animated)) {
    return (
      <div
        data-entity-card-preview=""
        className={cn(
          "entity-card-preview relative flex-1 min-h-0 overflow-hidden",
          onPreviewClick && "cursor-zoom-in",
        )}
        onClick={
          onPreviewClick
            ? (e) => {
                if (
                  (e.currentTarget as HTMLElement).closest("[data-selecting]")
                )
                  return;
                e.preventDefault();
                onPreviewClick();
              }
            : undefined
        }
      >
        {image && (
          <FadeInImage
            className="absolute inset-0 w-full h-full object-cover"
            src={image}
            alt=""
          />
        )}
        {idleMode === "animated" && animated && (
          <FadeInImage
            className="absolute inset-0 w-full h-full object-cover"
            src={animated}
            alt=""
          />
        )}
        {wantsVideoEl && video && !isTouch && (
          <video
            ref={videoRef}
            className={cn(
              "absolute inset-0 w-full h-full object-cover transition-opacity duration-200",
              idleMode === "video" || isHovered ? "opacity-100" : "opacity-0",
            )}
            src={video}
            poster={image ?? undefined}
            muted
            loop
            playsInline
            disableRemotePlayback
            preload={idleMode === "video" ? "auto" : "none"}
            autoPlay={idleMode === "video"}
          />
        )}
        {children}
      </div>
    );
  }

  const previewContent = (
    <div
      data-entity-card-preview=""
      className={cn(
        "entity-card-preview relative overflow-hidden bg-muted",
        isDetails
          ? "aspect-video w-32 shrink-0 self-stretch"
          : isPortrait
            ? "aspect-[2/3] w-full"
            : "aspect-video w-full",
        onPreviewClick && "cursor-zoom-in",
      )}
    >
      {/* Static screenshot — hidden when actively scrubbing */}
      {image &&
        !currentSprite &&
        (hasMismatch ? (
          <>
            {/* Plain <img> for the blur backdrop: it's already 60%
                opacity + heavily blurred, so its pop-in is barely
                perceptible. Wrapping it in FadeInImage would force its
                opacity to 0/100, fighting the static `opacity-60`. The
                foreground below shares the same src, so both finish at
                roughly the same time and the foreground's fade is what
                the eye actually tracks. */}
            <img
              className="absolute inset-0 h-full w-full object-cover scale-110 blur-md opacity-60"
              src={image}
              alt=""
              aria-hidden
            />
            <FadeInImage
              className="absolute inset-0 h-full w-full object-contain"
              src={image}
              alt=""
              onLoad={
                naturalIsPortraitProp === undefined
                  ? (e) =>
                      setNaturalIsPortrait(
                        e.currentTarget.naturalHeight >
                          e.currentTarget.naturalWidth,
                      )
                  : undefined
              }
            />
          </>
        ) : (
          <FadeInImage
            className={cn(
              "absolute inset-0 h-full w-full object-cover",
              isPortrait ? "object-top" : "object-center",
            )}
            src={image}
            alt=""
            onLoad={
              naturalIsPortraitProp === undefined
                ? (e) =>
                    setNaturalIsPortrait(
                      e.currentTarget.naturalHeight >
                        e.currentTarget.naturalWidth,
                    )
                : undefined
            }
          />
        ))}

      {/* VTT sprite tile (shown while scrubbing) — centered at native size */}
      {currentSprite && (
        <div className="absolute inset-0 flex items-center justify-center overflow-hidden bg-black">
          <div
            style={{
              backgroundImage: `url(${currentSprite.url})`,
              backgroundPosition: `-${currentSprite.x}px -${currentSprite.y}px`,
              backgroundSize: "auto",
              width: currentSprite.w,
              height: currentSprite.h,
            }}
          />
        </div>
      )}

      {/* Animated WebP idle overlay — fades over the static screenshot
          when `previewDefault` is "animated" (or "video" with no video
          but an animated fallback exists). Hidden while scrubbing. */}
      {idleMode === "animated" && animated && !currentSprite && (
        <FadeInImage
          className={cn(
            "absolute inset-0 h-full w-full",
            hasMismatch
              ? "object-contain"
              : cn("object-cover", isPortrait ? "object-top" : "object-center"),
          )}
          src={animated}
          alt=""
        />
      )}

      {/* Video preview — stacked on top of the idle layer. Only mounts
          when there is a video URL AND the config wants it (hover-swap
          enabled, or idle mode is "video"). */}
      {wantsVideoEl && video && !isTouch && (
        <video
          className={cn(
            "absolute inset-0 h-full w-full transition-opacity duration-200",
            hasMismatch
              ? "object-contain"
              : cn("object-cover", isPortrait ? "object-top" : "object-center"),
            idleMode === "video" || isHovered ? "opacity-100" : "opacity-0",
          )}
          ref={videoRef}
          src={video}
          poster={image ?? undefined}
          muted
          loop
          playsInline
          disableRemotePlayback
          preload={idleMode === "video" ? "auto" : "none"}
          autoPlay={idleMode === "video"}
        />
      )}

      {/* Top row: organized indicator + o-counter (left) + resolution
          badge (right). O-counter is display-only; the interactive +1
          control lives in the scene detail toolbar / lightbox overlay. */}
      <div className="pointer-events-none absolute inset-x-0 top-0 flex items-start justify-between gap-1 p-1.5">
        <div className="flex gap-1">
          {organized && (
            <span className={cn(badgeCls, "text-emerald-300")}>✓</span>
          )}
          {oCounter != null && oCounter > 0 && (
            <span
              className={cn(
                badgeCls,
                "inline-flex items-center gap-0.5 text-rose-200",
              )}
              aria-label={`O-count ${oCounter}`}
            >
              <Droplets className="size-2.5" aria-hidden />
              {oCounter}
            </span>
          )}
        </div>
        <div className="flex gap-1">
          {resolution && (
            <span className={badgeCls}>
              {resolutionLabel(resolution.width, resolution.height)}
            </span>
          )}
        </div>
      </div>

      {/* Bottom row: studio logo (left) + duration badge (right) */}
      <div className="pointer-events-none absolute inset-x-0 bottom-1.5 flex items-end justify-between gap-1 px-1.5">
        <div>
          {studioImagePath && !isMobile && (
            <img
              className="max-w-[2.5rem] rounded bg-black/60 object-contain px-0.5 py-0.5"
              style={{ height: "1.1rem" }}
              src={studioImagePath}
              alt=""
            />
          )}
        </div>
        <div>
          {duration != null && duration > 0 && (
            <span className={badgeCls}>{formatDuration(duration)}</span>
          )}
        </div>
      </div>

      {/* Rating ribbon (desktop only) */}
      {ratingBanner != null && !isMobile && (
        <div
          className="entity-card-rating-ribbon"
          data-rating={Math.round(ratingBanner / 20)}
        />
      )}

      {/* Resume progress bar */}
      {resumePercent != null && resumePercent > 0 && (
        <div className="absolute inset-x-0 bottom-0 h-[3px] bg-black/30">
          <div
            className="h-full bg-rose-500"
            style={{ width: `${resumePercent}%` }}
          />
        </div>
      )}

      {children}
    </div>
  );

  // Wrap with HoverScrubber when VTT sprites are available (hover-only UI)
  if (sprites && sprites.length > 0 && !isTouch) {
    return (
      <HoverScrubber count={sprites.length} onIndex={handleScrubIndex}>
        {previewContent}
      </HoverScrubber>
    );
  }

  return previewContent;
}

// ── Body ─────────────────────────────────────────────────────────────────────

function EntityCardBody({ children }: { children: React.ReactNode }) {
  const { isWall } = useContext(EntityCardCtx);
  if (isWall) return null;
  return (
    <div className="entity-card-body min-w-0 flex-1 flex flex-col gap-1 px-3 py-2.5">
      {children}
    </div>
  );
}

// ── Title ────────────────────────────────────────────────────────────────────

function useIsSingleCol(): boolean {
  const { isMobile, isDetails } = useContext(EntityCardCtx);
  const mobileGridCols = useMobileGridCols();
  return isDetails || (isMobile && mobileGridCols === 1);
}

// Watches an element's intrinsic vs visible width and reports whether its
// content is currently being truncated (overflowing past `clientWidth`).
// `scrollWidth` updates without triggering ResizeObserver when content
// changes inside a stable-sized box, so we also re-check when the content
// dep (e.g. the children string) changes.
function EntityCardTitle({ children }: { children: React.ReactNode }) {
  const singleCol = useIsSingleCol();
  const text = String(children ?? "");
  const [ref, truncated] = useIsTruncated<HTMLDivElement>();

  if (singleCol) {
    return (
      <div className="entity-card-title break-words font-medium leading-snug">
        {children}
      </div>
    );
  }

  return (
    <Tooltip disabled={!truncated}>
      <TooltipTrigger
        render={
          <div
            ref={ref}
            className="entity-card-title truncate font-medium leading-snug"
          >
            {children}
          </div>
        }
      />
      <TooltipContent>{text}</TooltipContent>
    </Tooltip>
  );
}

// ── Subtitle ─────────────────────────────────────────────────────────────────

function EntityCardSubtitle({
  children,
  noTooltip,
}: {
  children: React.ReactNode;
  /** Skip the truncation tooltip. Use for short, predictable content
   *  (e.g. dates) that's effectively guaranteed to fit. */
  noTooltip?: boolean;
}) {
  const singleCol = useIsSingleCol();
  const text = String(children ?? "");
  const [ref, truncated] = useIsTruncated<HTMLDivElement>();

  if (singleCol) {
    return (
      <div className="entity-card-subtitle break-words text-xs text-muted-foreground">
        {children}
      </div>
    );
  }

  if (noTooltip) {
    return (
      <div className="entity-card-subtitle truncate text-xs text-muted-foreground">
        {children}
      </div>
    );
  }

  return (
    <Tooltip disabled={!truncated}>
      <TooltipTrigger
        render={
          <div
            ref={ref}
            className="entity-card-subtitle truncate text-xs text-muted-foreground"
          >
            {children}
          </div>
        }
      />
      <TooltipContent>{text}</TooltipContent>
    </Tooltip>
  );
}

// ── Tags ─────────────────────────────────────────────────────────────────────

interface TagChip {
  id: string;
  name: string;
}

function EntityCardTags({ tags }: { tags?: TagChip[] | null }) {
  if (!tags?.length) return null;
  return (
    <div className="mt-0.5 flex flex-wrap gap-1">
      {tags.map((tag) => (
        <Link
          key={tag.id}
          className="rounded-sm bg-secondary px-1.5 py-0.5 text-[0.65rem] leading-none text-secondary-foreground transition-colors hover:bg-secondary/80"
          to="/tags/$tagId"
          params={{ tagId: tag.id }}
          onClick={(e) => e.stopPropagation()}
        >
          {tag.name}
        </Link>
      ))}
    </div>
  );
}

// ── Performers ────────────────────────────────────────────────────────────────

interface PerformerChip {
  id: string;
  name: string;
}

function EntityCardPerformers({
  performers,
}: {
  performers?: PerformerChip[] | null;
}) {
  if (!performers?.length) return null;
  return (
    <div className="mt-0.5 flex flex-wrap gap-1">
      {performers.map((p) => (
        <Link
          key={p.id}
          className="rounded-sm bg-secondary px-1.5 py-0.5 text-[0.65rem] leading-none text-secondary-foreground transition-colors hover:bg-secondary/80"
          to="/performers/$performerId"
          params={{ performerId: p.id }}
          onClick={(e) => e.stopPropagation()}
        >
          {p.name}
        </Link>
      ))}
    </div>
  );
}

// ── Rating ───────────────────────────────────────────────────────────────────

function EntityCardRating({ rating100 }: { rating100?: number | null }) {
  if (rating100 == null) return null;
  // Display as x.x / 5.0 stars (rating100 is 0-100)
  const stars = (rating100 / 20).toFixed(1);
  return (
    <div
      className="text-xs font-medium text-amber-500"
      title={`${rating100}/100`}
    >
      {stars}★
    </div>
  );
}

// ── SelectCheckbox / SelectOverlay ───────────────────────────────────────────
//
// In details/list view: render an explicit checkbox control.
// In grid view: render a translucent checkmark overlay on the preview corner.

function EntityCardSelectCheckbox() {
  const { isDetails, isWall, selected, onSelectedChanged } =
    useContext(EntityCardCtx);

  // Details view: classic checkbox. CSS ([data-selecting] .entity-card-checkbox)
  // controls visibility for selecting mode; .visible class handles selected state.
  if (isDetails) {
    return (
      <div
        className={cn("entity-card-checkbox", selected && "visible")}
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          onSelectedChanged?.(!selected, (e as React.MouseEvent).shiftKey);
        }}
      >
        <div
          className={cn(
            "size-4 rounded border border-border bg-background flex items-center justify-center transition-colors",
            selected && "bg-primary border-primary",
          )}
          aria-hidden="true"
        >
          {selected && (
            <Check
              size={11}
              className="text-primary-foreground"
              strokeWidth={3}
            />
          )}
        </div>
      </div>
    );
  }

  // Wall view: fully CSS-driven overlay — circle colour and check mark are
  // controlled by [data-selected="true"] on the article, set imperatively by
  // PhotoAlbumWall's useLayoutEffect. No React re-renders needed on selection change.
  if (isWall) {
    return (
      <div
        className="entity-card-select-overlay"
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          const article = (e.currentTarget as HTMLElement).closest<HTMLElement>(
            "article[data-id]",
          );
          onSelectedChanged?.(
            !(article?.dataset.selected === "true"),
            e.shiftKey,
          );
        }}
      >
        <div className="entity-card-wall-circle" aria-hidden="true">
          <Check
            size={12}
            className="entity-card-wall-check text-primary-foreground"
            strokeWidth={3}
          />
        </div>
      </div>
    );
  }

  // Grid view: translucent overlay circle in top-left corner of preview.
  // Always rendered — CSS ([data-selecting] .entity-card-select-overlay and
  // [data-selected="true"] .entity-card-select-overlay) controls display.
  return (
    <div
      className="entity-card-select-overlay"
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        onSelectedChanged?.(!selected, e.shiftKey);
      }}
    >
      <div
        className={cn(
          "size-5 rounded-full border-2 border-white/80 flex items-center justify-center transition-colors shadow-sm",
          selected ? "bg-primary border-primary" : "bg-black/30",
        )}
        aria-hidden="true"
      >
        {selected && (
          <Check
            size={12}
            className="text-primary-foreground"
            strokeWidth={3}
          />
        )}
      </div>
    </div>
  );
}

// ── Compound export ───────────────────────────────────────────────────────────

export const EntityCard = Object.assign(EntityCardRoot, {
  Preview: EntityCardPreview,
  Body: EntityCardBody,
  Title: EntityCardTitle,
  Subtitle: EntityCardSubtitle,
  Tags: EntityCardTags,
  Performers: EntityCardPerformers,
  Rating: EntityCardRating,
  SelectCheckbox: EntityCardSelectCheckbox,
});

export type {
  EntityCardRootProps,
  EntityCardPreviewProps,
  TagChip,
  PerformerChip,
};
