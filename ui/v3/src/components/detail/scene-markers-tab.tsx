/**
 * Editable scene markers tab. Replaces the v2.5-style read-only list
 * with an inline editor (`MarkerEditPanel`) that swaps in for the list
 * during create / edit, plus a small confirm dialog for delete. The
 * editor is rendered inline rather than in an overlay surface (Sheet /
 * Drawer) because both blur and absorb touches under them, which would
 * make the player's slider drag handles unreachable.
 *
 * Three timestamp affordances on each start / end input — typed
 * `mm:ss.fff`, "use current time" clock button, and drag-the-handle on
 * the player's position slider — are coordinated through
 * `getCurrentTime`, `onBoundsChange`, and `registerBoundSetter` props
 * piped down from the page.
 */
import { useEffect, useState } from "react";
import { useMutation } from "@apollo/client/react";
import { useIntl } from "react-intl";
import { Link } from "@tanstack/react-router";
import {
  ArrowLeftIcon,
  BookmarkIcon,
  PencilIcon,
  PlusIcon,
  Trash2Icon,
} from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { cn } from "src/lib/utils";
import { Badge } from "src/components/ui/badge";
import { Button } from "src/components/ui/button";
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  EmptyDescription,
} from "src/components/ui/empty";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import { secondsToTimestamp } from "src/utils/duration";
import { MarkerEditPanel } from "./marker-edit-panel";
import type { MarkerBoundary, MarkerFormBounds } from "./marker-edit-form";

// Push / pop transition between the markers list and the inline editor.
// `opening` slides the list left-and-out while the editor enters from the
// right; `closing` slides the editor right-and-out while the list
// re-enters from the left. Duration must match the CSS animation duration
// applied to the panels (200 ms) — the timeout drops the outgoing panel
// from the DOM right when its `animate-out` finishes.
type EditorTransitionPhase = "list" | "opening" | "editor" | "closing";
const EDITOR_TRANSITION_MS = 200;

type SceneData = NonNullable<GQL.FindSceneQuery["findScene"]>;
type SceneMarker = SceneData["scene_markers"][number];

interface SceneMarkersTabProps {
  scene: SceneData;
  /** Seek the player to a given scene-time. */
  onSeek?: (seconds: number) => void;
  /** Reads the player's current playhead — wired to the editor's "use
   *  current time" button. */
  getCurrentTime?: () => number | undefined;
  /** Emitted when the editor's start / end values change so the
   *  player's position-slider handles can track them. */
  onBoundsChange?: (bounds: MarkerFormBounds) => void;
  /** Hands the page a writer that updates the editor's start / end when
   *  the user drags a handle on the position slider. */
  registerBoundSetter?: (
    setter: (boundary: MarkerBoundary, time: number) => void,
  ) => void;
}

export function SceneMarkersTab({
  scene,
  onSeek,
  getCurrentTime,
  onBoundsChange,
  registerBoundSetter,
}: SceneMarkersTabProps) {
  const intl = useIntl();

  const [phase, setPhase] = useState<EditorTransitionPhase>("list");
  // null while phase === 'list' OR while creating a new marker. Captured
  // at open-time so the form's `key` swap on edit-vs-create remounts
  // cleanly.
  const [editingMarker, setEditingMarker] = useState<SceneMarker | null>(null);
  const [pendingDelete, setPendingDelete] = useState<SceneMarker | null>(null);

  const [destroyMarker, { loading: destroying }] = useMutation(
    GQL.SceneMarkerDestroyDocument,
    {
      refetchQueries: [
        { query: GQL.FindSceneDocument, variables: { id: scene.id } },
      ],
    },
  );

  // Auto-advance the transition phase machine: `opening → editor` and
  // `closing → list` after the slide animation finishes. Marker reset
  // happens on `closing → list` so the form keeps rendering its current
  // marker through the slide-out (otherwise the editor would briefly
  // flash an empty form during the closing animation).
  useEffect(() => {
    if (phase === "opening") {
      const t = setTimeout(() => setPhase("editor"), EDITOR_TRANSITION_MS);
      return () => clearTimeout(t);
    }
    if (phase === "closing") {
      const t = setTimeout(() => {
        setPhase("list");
        setEditingMarker(null);
      }, EDITOR_TRANSITION_MS);
      return () => clearTimeout(t);
    }
  }, [phase]);

  // Player handles need to disappear once the editor closes. The form's
  // own `onBoundsChange` effect stops firing on unmount but the page's
  // last-seen bounds linger; emit an explicit `{ null, null }` clear
  // here so the page-level bounds state resets when the editor finishes
  // unmounting.
  useEffect(() => {
    if (phase === "list") {
      onBoundsChange?.({ start: null, end: null });
    }
  }, [phase, onBoundsChange]);

  function openCreate() {
    setEditingMarker(null);
    setPhase("opening");
  }
  function openEdit(marker: SceneMarker) {
    setEditingMarker(marker);
    setPhase("opening");
  }
  function closeEditor() {
    setPhase("closing");
  }

  async function confirmDelete() {
    if (!pendingDelete) return;
    await destroyMarker({ variables: { id: pendingDelete.id } });
    setPendingDelete(null);
  }

  // Group markers by primary tag — the marker list reads as a "what
  // tagged the scene at which moments" outline rather than a time-only
  // strip. Within each group, markers are ordered by start time.
  // Groups themselves are ordered by primary-tag name (alphabetical).
  const markersAll = [...scene.scene_markers];
  const groups = (() => {
    const byTag = new Map<
      string,
      { tag: SceneMarker["primary_tag"]; markers: SceneMarker[] }
    >();
    for (const m of markersAll) {
      const existing = byTag.get(m.primary_tag.id);
      if (existing) existing.markers.push(m);
      else byTag.set(m.primary_tag.id, { tag: m.primary_tag, markers: [m] });
    }
    return [...byTag.values()]
      .sort((a, b) => a.tag.name.localeCompare(b.tag.name))
      .map((g) => ({
        ...g,
        markers: g.markers.slice().sort((a, b) => a.seconds - b.seconds),
      }));
  })();

  // Editing surface: replaces the list while open. Inline (rather than a
  // Sheet / Drawer) on every screen size — overlay surfaces blur and
  // absorb touches under them, which would block the slider drag handles.
  // Both panels are rendered during `opening` / `closing` so each can
  // animate independently; outside the transition only one is mounted.
  const showList = phase !== "editor";
  const showEditor = phase !== "list";

  // Push/pop transition: outgoing panel is taken out of flow with
  // `absolute inset-0` so the incoming panel drives parent height during
  // the animation. `[animation-fill-mode:forwards]` pins the outgoing
  // panel at its end-state (slid off, faded out) until our state
  // machine drops it from the DOM at +EDITOR_TRANSITION_MS.
  const listClassName = cn(
    "flex flex-col gap-3",
    phase === "opening" &&
      "absolute inset-0 animate-out fade-out-0 slide-out-to-left-8 duration-200 [animation-fill-mode:forwards]",
    phase === "closing" &&
      "animate-in fade-in-0 slide-in-from-left-8 duration-200",
  );
  const editorClassName = cn(
    phase === "opening" &&
      "animate-in fade-in-0 slide-in-from-right-8 duration-200",
    phase === "closing" &&
      "absolute inset-0 animate-out fade-out-0 slide-out-to-right-8 duration-200 [animation-fill-mode:forwards]",
  );

  const editorHeading = (
    <div className="flex items-center gap-2">
      <Button
        variant="ghost"
        size="icon-sm"
        onClick={closeEditor}
        title={intl.formatMessage({
          id: "actions.back",
          defaultMessage: "Back",
        })}
      >
        <ArrowLeftIcon />
      </Button>
      <h2 className="text-base font-semibold">
        {editingMarker
          ? intl.formatMessage({
              id: "actions.edit_marker",
              defaultMessage: "Edit marker",
            })
          : intl.formatMessage({
              id: "actions.new_marker",
              defaultMessage: "New marker",
            })}
      </h2>
    </div>
  );

  return (
    <div className="relative">
      {showList && (
        <div className={listClassName}>
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">
              {intl.formatMessage(
                {
                  id: "markers_count_summary",
                  defaultMessage:
                    "{count, plural, =0 {No markers} one {# marker} other {# markers}}",
                },
                { count: markersAll.length },
              )}
            </span>
            <Button size="sm" onClick={openCreate}>
              <PlusIcon />
              {intl.formatMessage({
                id: "actions.new_marker",
                defaultMessage: "New marker",
              })}
            </Button>
          </div>

          {groups.length === 0 && (
            <Empty className="border border-dashed border-border rounded-lg">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <BookmarkIcon />
                </EmptyMedia>
                <EmptyTitle>
                  {intl.formatMessage({
                    id: "scene_markers.empty.title",
                    defaultMessage: "No markers yet",
                  })}
                </EmptyTitle>
                <EmptyDescription>
                  {intl.formatMessage({
                    id: "scene_markers.empty.description",
                    defaultMessage:
                      "Bookmark moments with markers — use New marker above to create one.",
                  })}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}

          {groups.length > 0 && (
            <div className="flex flex-col gap-5">
              {groups.map(({ tag, markers }) => (
                <section key={tag.id} className="flex flex-col gap-2">
                  <Link
                    to="/tags/$tagId"
                    params={{ tagId: tag.id }}
                    className="text-sm font-semibold hover:underline self-start"
                  >
                    {tag.name}
                    <span className="ml-1.5 text-muted-foreground font-normal">
                      ({markers.length})
                    </span>
                  </Link>
                  <ul className="flex flex-col gap-2 list-none m-0 p-0">
                    {markers.map((marker) => (
                      <li
                        key={marker.id}
                        className="flex items-start gap-3 border border-border rounded-md p-2"
                      >
                        <Button
                          variant="ghost"
                          onClick={() => onSeek?.(marker.seconds)}
                          title={intl.formatMessage({
                            id: "seek_to_marker",
                            defaultMessage: "Seek to marker",
                          })}
                          className="shrink-0 h-auto w-auto p-0 rounded overflow-hidden bg-transparent hover:bg-transparent"
                        >
                          <img
                            src={marker.screenshot}
                            alt={marker.title}
                            loading="lazy"
                            className="rounded block h-20 w-32 object-cover"
                          />
                        </Button>
                        <div className="flex-auto min-w-0 flex flex-col gap-1.5">
                          <Button
                            variant="outline"
                            size="xs"
                            onClick={() => onSeek?.(marker.seconds)}
                            title={intl.formatMessage({
                              id: "seek_to_marker",
                              defaultMessage: "Seek to marker",
                            })}
                            className="self-start font-mono text-primary"
                          >
                            {secondsToTimestamp(marker.seconds, true)}
                            {marker.end_seconds != null && (
                              <span className="text-muted-foreground">
                                {" "}
                                – {secondsToTimestamp(marker.end_seconds, true)}
                              </span>
                            )}
                          </Button>
                          {marker.title && marker.title !== tag.name && (
                            <div className="text-sm font-medium break-words">
                              {marker.title}
                            </div>
                          )}
                          {marker.tags.length > 0 && (
                            <div className="flex flex-wrap gap-1">
                              {marker.tags.map((t) => (
                                <Badge
                                  key={t.id}
                                  variant="secondary"
                                  render={
                                    <Link
                                      to="/tags/$tagId"
                                      params={{ tagId: t.id }}
                                    />
                                  }
                                >
                                  {t.name}
                                </Badge>
                              ))}
                            </div>
                          )}
                        </div>
                        <div className="flex flex-col gap-1 shrink-0">
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            onClick={() => openEdit(marker)}
                            title={intl.formatMessage({
                              id: "actions.edit",
                              defaultMessage: "Edit",
                            })}
                          >
                            <PencilIcon />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            onClick={() => setPendingDelete(marker)}
                            title={intl.formatMessage({
                              id: "actions.delete",
                              defaultMessage: "Delete",
                            })}
                            className="text-destructive hover:text-destructive"
                          >
                            <Trash2Icon />
                          </Button>
                        </div>
                      </li>
                    ))}
                  </ul>
                </section>
              ))}
            </div>
          )}
        </div>
      )}

      {showEditor && (
        <div className={editorClassName}>
          <MarkerEditPanel
            sceneId={scene.id}
            marker={editingMarker}
            getCurrentTime={getCurrentTime}
            onBoundsChange={onBoundsChange}
            registerBoundSetter={registerBoundSetter}
            onSaved={closeEditor}
            onCancel={closeEditor}
            heading={editorHeading}
          />
        </div>
      )}

      <Dialog
        open={pendingDelete != null}
        onOpenChange={(o) => {
          if (!o && !destroying) setPendingDelete(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {intl.formatMessage({
                id: "dialogs.delete_marker_title",
                defaultMessage: "Delete marker?",
              })}
            </DialogTitle>
            <DialogDescription>
              {pendingDelete?.title || pendingDelete?.primary_tag.name}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setPendingDelete(null)}
              disabled={destroying}
            >
              {intl.formatMessage({
                id: "actions.cancel",
                defaultMessage: "Cancel",
              })}
            </Button>
            <Button
              variant="destructive"
              onClick={confirmDelete}
              disabled={destroying}
            >
              <Trash2Icon />
              {intl.formatMessage({
                id: "actions.delete",
                defaultMessage: "Delete",
              })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
