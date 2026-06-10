/**
 * Inline detail-vs-edit transition wrapper. Mirrors the marker-editor
 * pattern in `scene-markers-tab.tsx` — when `editing` flips, the
 * outgoing panel slides off (left for the detail view → off-screen, or
 * right for the edit form → off-screen) while the incoming panel
 * slides in from the opposite edge. Both panels are mounted only
 * during the cross-fade window; outside that, only the active one is
 * in the DOM.
 *
 * Used by the entity detail routes (performer / studio / tag / group)
 * to present the edit form as an inline pane that animates into the
 * detail-page surface, instead of the previous side-Sheet that
 * overlaid the whole viewport.
 */
import type React from "react";
import { useEffect, useState } from "react";
import { cn } from "src/lib/utils";

const TRANSITION_MS = 200;

type Phase = "detail" | "opening" | "edit" | "closing";

export interface DetailEditTransitionProps {
  editing: boolean;
  detail: React.ReactNode;
  editForm: React.ReactNode;
  className?: string;
  /**
   * Stretch the wrapper + panels to `h-full`. Use when the parent
   * provides a defined height (e.g. the detail aside, which is
   * `md:flex md:flex-col` inside an `md:h-full` row), so the edit
   * form can fill it and own its own internal scroll. Leave off
   * when the parent is content-sized (e.g. a tab pane) — both
   * panels then render at their natural height and the parent /
   * outer scroll container handles overflow.
   */
  fillHeight?: boolean;
}

export function DetailEditTransition({
  editing,
  detail,
  editForm,
  className,
  fillHeight = false,
}: DetailEditTransitionProps) {
  const [phase, setPhase] = useState<Phase>(editing ? "edit" : "detail");

  // Sync the phase machine with the controlled `editing` prop.
  useEffect(() => {
    setPhase((p) => {
      if (editing && p !== "edit" && p !== "opening") return "opening";
      if (!editing && p !== "detail" && p !== "closing") return "closing";
      return p;
    });
  }, [editing]);

  // Auto-advance opening → edit and closing → detail after the slide
  // animation completes.
  useEffect(() => {
    if (phase === "opening") {
      const t = setTimeout(() => setPhase("edit"), TRANSITION_MS);
      return () => clearTimeout(t);
    }
    if (phase === "closing") {
      const t = setTimeout(() => setPhase("detail"), TRANSITION_MS);
      return () => clearTimeout(t);
    }
  }, [phase]);

  const showDetail = phase !== "edit";
  const showEdit = phase !== "detail";

  // Push/pop transition: outgoing panel is taken out of flow with
  // `absolute inset-0` so the incoming panel drives the parent's
  // height during the animation. `[animation-fill-mode:forwards]`
  // pins the outgoing panel at its end-state until our state
  // machine drops it from the DOM.
  // When `fillHeight` is on, both panels render at full parent height and
  // are flex columns so children using `flex-1 min-h-0 overflow-y-auto`
  // can claim the remaining height and own their own scroll. (The edit
  // form already wraps its content in `flex flex-col h-full`; the detail
  // pane relies on this wrapper to provide the same context.)
  // The flash before the edit form slid in came from the detail panel
  // going `absolute` on mobile, which collapsed the aside and shifted
  // the entity list up into the gap left in viewport. Keeping detail in
  // flow on mobile (gating `absolute inset-0` to `md:`) holds the page
  // layout still while the edit overlay animates in over it — the slide
  // animations can stay as they were.
  const detailClassName = cn(
    "w-full",
    fillHeight && "h-full flex flex-col",
    phase === "opening" &&
      "md:absolute md:inset-0 animate-out fade-out-0 slide-out-to-left-8 duration-200 [animation-fill-mode:forwards]",
    phase === "closing" &&
      "animate-in fade-in-0 slide-in-from-left-8 duration-200",
  );
  const editClassName = cn(
    "w-full",
    fillHeight && "h-full flex flex-col",
    // On mobile, the detail aside sits in the page's normal scroll flow
    // alongside the entity tab content (list view + mobile chrome bar).
    // Lift the edit panel out of that flow into a full-viewport overlay
    // so the form covers the list and the chrome bar entirely. Mobile
    // chrome bar uses z-50; we use z-[60] to sit above. Forces flex-col
    // layout regardless of `fillHeight` so the form's anchored action bar
    // works.
    "max-md:fixed max-md:inset-0 max-md:z-[60] max-md:bg-background max-md:h-full max-md:flex max-md:flex-col",
    phase === "opening" &&
      "animate-in fade-in-0 slide-in-from-right-8 duration-200",
    phase === "closing" &&
      // Desktop: absolute inset-0 takes the outgoing edit panel out of
      // flow so the incoming detail can drive parent height. On mobile
      // the panel stays fixed inset-0 (from the max-md: rule above) so
      // the slide-out plays from the same overlay position the panel
      // animated into.
      "md:absolute md:inset-0 animate-out fade-out-0 slide-out-to-right-8 duration-200 [animation-fill-mode:forwards]",
  );

  return (
    <div className={cn("relative w-full", fillHeight && "h-full", className)}>
      {showDetail && <div className={detailClassName}>{detail}</div>}
      {showEdit && <div className={editClassName}>{editForm}</div>}
    </div>
  );
}
