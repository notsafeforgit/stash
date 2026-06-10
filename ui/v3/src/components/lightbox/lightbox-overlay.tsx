/**
 * Visual shell shared by `SceneOverlay` (scene lightbox) and
 * `ImageEntityFooter` (image lightbox). Provides:
 *
 *   - Absolute positioning at top or bottom of the slide.
 *   - Tight `text-shadow` halo + `drop-shadow` on SVGs so content
 *     stays readable against any image / video frame.
 *   - Hover-only gradient (gated on `(hover: hover)`) for desktop
 *     contrast; on touch the gradient never shows so it can't feel
 *     like a permanent dark band.
 *   - `pointer-events: none` by default so video-area clicks pass
 *     through; `(hover: hover)` opts the wrapper back in so child
 *     `pointer-events: auto` regions can capture hover.
 *
 * `passThrough` mode disables the wrapper's `pointer-events: auto`
 * gating entirely so events fall through to the layer below (used by
 * the scene lightbox so mouse-over-overlay doesn't steal the video
 * player's user-activity tracking and prematurely hide the controls).
 * In this mode the gradient is driven by the explicit `gradientVisible`
 * prop instead of CSS `:hover`, since `:hover` won't fire on a
 * `pointer-events: none` element. Interactive children must opt back in
 * with `pointer-events: auto`.
 */
import type React from "react";
import { cn } from "src/lib/utils";

interface LightboxOverlayProps {
  /** Top: title / metadata. Bottom: footer / counters. */
  position: "top" | "bottom";
  className?: string;
  children: React.ReactNode;
  /** See module comment. */
  passThrough?: boolean;
  /** Required when `passThrough` — gradient opacity. */
  gradientVisible?: boolean;
}

export function LightboxOverlay({
  position,
  className,
  children,
  passThrough = false,
  gradientVisible = false,
}: LightboxOverlayProps) {
  const isTop = position === "top";
  return (
    <div
      className={cn(
        "group/lightbox-overlay absolute left-0 right-0 z-20 px-4 flex flex-col gap-2 text-white/90",
        passThrough
          ? "pointer-events-none"
          : "pointer-events-none [@media(hover:hover)]:pointer-events-auto",
        "[&_svg]:[filter:drop-shadow(0_1px_2px_rgb(0_0_0_/_0.9))]",
        // Top variant leaves room for YARL's top-right toolbar
        // (fullscreen + close); bottom variant is unobstructed.
        // `lightbox-overlay-top` / `lightbox-overlay-bottom` are
        // stable hooks for media-query overrides (e.g. tap-to-reveal
        // on mobile — see globals.css).
        isTop
          ? "lightbox-overlay-top top-0 pt-3 pb-10 pr-20"
          : "lightbox-overlay-bottom bottom-0 pt-8 pb-3",
        className,
      )}
      // Only stop propagation when the tap landed on an interactive
      // descendant (button / link / input / [role=button]) — those
      // need to swallow the click so it doesn't also re-toggle the
      // parent's tap-to-reveal state and hide the chrome out from
      // under the user. Everywhere else inside the overlay (gap
      // areas between badges, the details text block, etc.) should
      // bubble the click up to YARL's `on.click` so a tap there
      // hides the chrome the same way a tap on the bare image
      // does. Without this carve-out the gap areas felt "dead" —
      // the user couldn't dismiss the chrome from anywhere except
      // the bare image surface.
      onClick={(e) => {
        if (
          e.target instanceof Element &&
          e.target.closest(
            "button, a, input, textarea, select, [role='button']",
          )
        ) {
          e.stopPropagation();
        }
      }}
      style={{
        textShadow: "0 1px 2px rgb(0 0 0 / 0.9), 0 0 6px rgb(0 0 0 / 0.6)",
      }}
    >
      <div
        className={cn(
          "pointer-events-none absolute inset-0 -z-10 transition-opacity duration-200",
          passThrough
            ? gradientVisible
              ? "opacity-100"
              : "opacity-0"
            : "opacity-0 group-hover/lightbox-overlay:opacity-100",
          isTop
            ? "bg-gradient-to-b from-black/70 to-transparent"
            : "bg-gradient-to-t from-black/70 to-transparent",
        )}
      />
      {children}
    </div>
  );
}

/**
 * Long-text details block for use inside `<LightboxOverlay>`.
 *
 * Desktop: capped at one fifth of viewport width and ~40 % of viewport
 * height with internal vertical scroll so multi-paragraph descriptions
 * don't crowd the player area. Wheel events that hit the box scroll
 * its own contents only — `onWheel` stops propagation so they never
 * reach YARL's image-zoom / carousel handlers, and the unstoppable
 * `overscroll-contain` keeps the browser from chaining scroll out to
 * the page once the box hits its edges. Wheel events outside the box
 * never reach it (parent overlay is `pointer-events: none`), so they
 * pan / zoom the image as usual.
 *
 * Mobile (coarse pointer): expands to the full width the parent
 * overlay allows (the parent already pads for the YARL toolbar), so
 * the block reads across the screen instead of squeezing into a
 * column on the left. For top-positioned overlays it also truncates
 * with `line-clamp` rather than scrolling — the scene player's big
 * skip / play touch buttons sit at viewport-center, and a tall
 * scrolling details box would extend down into them.
 *
 * `whitespace-pre-wrap` matches how scene/image detail tabs render
 * the same field, so newlines authored in the metadata survive into
 * the lightbox view too. `pointer-events-auto` so the user can
 * scroll / select inside the overlay (the parent overlay opts out by
 * default to let video clicks pass through).
 */
export function LightboxDetails({
  text,
  position = "top",
}: {
  text: string;
  position?: "top" | "bottom";
}) {
  const trimmed = text.trim();
  if (!trimmed) return null;
  return (
    <div
      className={cn(
        "pointer-events-auto select-text whitespace-pre-wrap text-xs leading-relaxed text-white/85 pr-1 overscroll-contain",
        "[@media(pointer:fine)]:max-w-[20vw] [@media(pointer:fine)]:max-h-[40vh] [@media(pointer:fine)]:overflow-y-auto",
        // The top-position parent reserves `pr-20` to clear the YARL
        // fullscreen / close toolbar at top-right. The details block is the
        // last item in the overlay stack — well below the toolbar's vertical
        // region — so on mobile we cancel that clearance with a negative
        // margin so left and right margins look symmetric instead of
        // gutter-heavy on the right.
        position === "top"
          ? "[@media(pointer:coarse)]:line-clamp-8 [@media(pointer:coarse)]:-mr-16"
          : "[@media(pointer:coarse)]:max-h-[30vh] [@media(pointer:coarse)]:overflow-y-auto",
      )}
      // Wheel inside the box scrolls only the box: `stopPropagation`
      // keeps YARL's zoom / pan plugin (and the scene-lightbox's
      // horizontal-wheel slide-advance lockout) from acting on the
      // same gesture.
      onWheel={(e) => e.stopPropagation()}
      // The shared overlay text-shadow makes long-form copy hard to
      // read against bright frames; a low-alpha inline background on
      // the details box specifically gives the text a stable backing
      // without tinting the whole overlay region.
      style={{
        background:
          "linear-gradient(to right, rgb(0 0 0 / 0.7), rgb(0 0 0 / 0.55))",
        backdropFilter: "blur(2px)",
        borderRadius: 4,
        padding: "6px 8px",
      }}
    >
      {trimmed}
    </div>
  );
}
