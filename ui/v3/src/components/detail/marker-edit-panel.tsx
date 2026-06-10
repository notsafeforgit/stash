/**
 * Inline panel that hosts `<MarkerEditForm>` — used for both create and
 * edit. Renders directly inside the markers tab so the player and its
 * position-slider drag handles stay usable on every layout:
 *   - Desktop: tabs panel sits in a 420 px left column; the editor
 *     replaces the list while editing, the player on the right stays
 *     untouched.
 *   - Mobile / tablet: tab content is stacked below the player; the
 *     editor occupies the tab content area while editing. The player
 *     above stays visible and interactive (a `Sheet` / `Drawer` would
 *     blur and absorb touches under it, blocking the drag handles).
 *
 * Bidirectional bound binding (form ↔ player slider handles) is owned
 * by the form itself — see `MarkerEditForm`'s `onBoundsChange` /
 * `registerBoundSetter` props.
 */
import type React from "react";
import type * as GQL from "src/core/generated-graphql";
import {
  MarkerEditForm,
  type MarkerBoundary,
  type MarkerFormBounds,
} from "./marker-edit-form";

type SceneMarker = GQL.SceneMarkerDataFragment;

export interface MarkerEditPanelProps {
  sceneId: string;
  /** Existing marker for edit mode; null for create. */
  marker: SceneMarker | null;
  /** Reads the player's current playhead — wired to the page-level
   *  `getCurrentTime` getter. */
  getCurrentTime?: () => number | undefined;
  /** Emitted on every change to the form's start / end fields. */
  onBoundsChange?: (bounds: MarkerFormBounds) => void;
  /** Registers a writer the page can call when a slider handle is
   *  dragged so the form's field updates. */
  registerBoundSetter?: (
    setter: (boundary: MarkerBoundary, time: number) => void,
  ) => void;
  /** Save / cancel callbacks the form's action bar invokes — typically
   *  both transition back to the markers list. */
  onSaved: () => void;
  onCancel: () => void;
  /** Optional heading row (e.g. "New marker" / "Edit marker") rendered
   *  above the form. */
  heading?: React.ReactNode;
}

export function MarkerEditPanel({
  sceneId,
  marker,
  getCurrentTime,
  onBoundsChange,
  registerBoundSetter,
  onSaved,
  onCancel,
  heading,
}: MarkerEditPanelProps) {
  return (
    <div className="flex flex-col gap-3">
      {heading}
      {/* Re-mount on marker change so TanStack Form's `defaultValues`
          are picked up cleanly — its imperative reset path is fiddly
          and a fresh mount keeps the create / edit transitions clean. */}
      <MarkerEditForm
        key={marker?.id ?? "new"}
        sceneId={sceneId}
        marker={marker}
        getCurrentTime={getCurrentTime}
        onBoundsChange={onBoundsChange}
        registerBoundSetter={registerBoundSetter}
        onSaved={onSaved}
        onCancel={onCancel}
      />
    </div>
  );
}
