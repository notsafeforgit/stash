import type React from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import {
  closestCenter,
  type CollisionDetection,
  DndContext,
  type DragEndEvent,
  type DragOverEvent,
  DragOverlay,
  type DragStartEvent,
  type Modifier,
  PointerSensor,
  pointerWithin,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { Check, Copy, Filter, Lock } from "lucide-react";
import { Button } from "src/components/ui/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "src/components/ui/empty";
import { FormattedMessage, useIntl } from "react-intl";
import {
  createASTGroup,
  decodeFilterASTNode,
  encodeFilterASTNode,
  type FilterASTConditionNode,
  type FilterASTGroupNode,
} from "src/models/list-filter/filter-ast";
import type { FilterMode } from "src/core/generated-graphql";
import type { ListFilterModel } from "src/models/list-filter/filter";
import type { View } from "src/components/list/views";
import { useMediaQuery } from "src/utils/screen";
import {
  type ASTDragItem,
  type ASTDropTarget,
  containsGroupId,
  findASTNodePreview,
  findGroupById,
  findParentOf,
  getEventClientY,
  moveASTNode,
  parsePlaceholderID,
  wouldExceedDepth,
} from "./filter-builder-types";
import { usePinnedFields } from "./condition-editor";
import { GroupEditor } from "./group-editor";
import { FilterDragPreview } from "./filter-drag-preview";
import { SavedFilterBar } from "./saved-filter-bar";

// ── Locked conditions read-only display ───────────────────────────────────────

/**
 * Renders the context-locked filter conditions (e.g. "Performer: Jane Doe")
 * as read-only chips at the top of the filter panel. These cannot be removed
 * or edited by the user.
 */
const LockedConditions: React.FC<{ lockedRoot: FilterASTGroupNode }> = ({
  lockedRoot,
}) => {
  const intl = useIntl();

  // Collect all leaf condition nodes recursively
  function collectConditions(
    node: FilterASTGroupNode,
  ): FilterASTConditionNode[] {
    const results: FilterASTConditionNode[] = [];
    for (const child of node.children) {
      if (child.kind === "condition") {
        results.push(child);
      } else {
        results.push(...collectConditions(child));
      }
    }
    return results;
  }

  const conditions = collectConditions(lockedRoot);
  if (conditions.length === 0) return null;

  return (
    <div className="flex flex-col gap-1 px-3 pt-3">
      <p className="text-xs text-muted-foreground font-medium uppercase tracking-wide">
        <FormattedMessage
          id="search_filter.locked_filters"
          defaultMessage="Context filters"
        />
      </p>
      <div className="flex flex-wrap gap-1.5">
        {conditions.map((cond) => (
          <span
            key={cond.id}
            className="inline-flex items-center gap-1 rounded-md border border-border bg-muted px-2 py-1 text-xs text-muted-foreground"
            title={intl.formatMessage({
              id: "search_filter.locked_filter_tooltip",
              defaultMessage:
                "This filter is locked by the current context and cannot be removed.",
            })}
          >
            <Lock size={10} className="shrink-0" />
            {cond.criterion.getLabel(intl)}
          </span>
        ))}
      </div>
    </div>
  );
};

// ── FilterBuilder ──────────────────────────────────────────────────────────────

export const FilterBuilder: React.FC<{
  mode: FilterMode;
  filter: ListFilterModel;
  setFilter: (filter: ListFilterModel) => void;
  root?: FilterASTGroupNode;
  onChange: (node?: FilterASTGroupNode) => void;
  isOpen?: boolean;
  currentSavedFilterName?: string;
  onCurrentSavedFilterChange: (next?: {
    id?: string;
    name: string;
    justApplied?: boolean;
  }) => void;
  /** Locked context filter conditions rendered read-only above the editable area. */
  lockedRoot?: FilterASTGroupNode;
  /** When provided, shows controls to set/clear the default filter for this view. */
  view?: View;
}> = ({
  mode,
  filter,
  setFilter,
  root,
  onChange,
  isOpen = false,
  currentSavedFilterName,
  onCurrentSavedFilterChange,
  lockedRoot,
  view,
}) => {
  const isNarrow = useMediaQuery("only screen and (max-width: 767px)");
  const [copied, setCopied] = useState(false);
  const copiedTimerRef = useRef<number | undefined>(undefined);
  const [pasteError, setPasteError] = useState<string>();
  const pasteErrorTimerRef = useRef<number | undefined>(undefined);
  const { pinnedFields, togglePinnedField } = usePinnedFields(mode);
  const builderRef = useRef<HTMLDivElement>(null);
  const autoScrollFrameRef = useRef<number | undefined>(undefined);
  const autoScrollVelocityRef = useRef(0);
  const autoScrollContainerRef = useRef<HTMLElement | null>(null);
  const dragScrollContainerRef = useRef<HTMLElement | null>(null);
  const dragStartScrollTopRef = useRef(0);
  const pointerYRef = useRef<number | undefined>(undefined);
  const dragGrabOffsetYRef = useRef<number>(0);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
  );
  const [draggedItem, setDraggedItem] = useState<ASTDragItem>();
  const [crossGroupInsert, setCrossGroupInsert] = useState<
    { groupId: string; index: number } | undefined
  >();
  const crossGroupInsertRef = useRef<
    { groupId: string; index: number } | undefined
  >(undefined);

  // Paste-to-load: when the pane is open and the user pastes JSON produced by
  // the copy button (or manually crafted), decode it and replace the filter.
  useEffect(() => {
    if (!isOpen) return;

    function handlePaste(e: ClipboardEvent) {
      const target = e.target as HTMLElement;
      if (
        target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.isContentEditable
      ) {
        return;
      }
      const text = e.clipboardData?.getData("text/plain");
      if (!text) return;
      try {
        const decoded = decodeFilterASTNode(mode, JSON.parse(text));
        if (decoded.kind !== "group") throw new Error("root must be a group");
        onChange(decoded as FilterASTGroupNode);
      } catch {
        window.clearTimeout(pasteErrorTimerRef.current);
        setPasteError("Pasted text is not a valid filter");
        pasteErrorTimerRef.current = window.setTimeout(
          () => setPasteError(undefined),
          3000,
        );
      }
    }

    document.addEventListener("paste", handlePaste);
    return () => document.removeEventListener("paste", handlePaste);
  }, [isOpen, mode, onChange]);

  const dragOverlayModifiers = useMemo<Modifier[]>(
    () => [
      ({ transform }) => ({
        ...transform,
        y: transform.y + dragGrabOffsetYRef.current - 20,
      }),
    ],
    [],
  );

  const collisionDetection = useCallback<CollisionDetection>((args) => {
    const edgeZones = args.droppableContainers.filter((c) => {
      const id = String(c.id);
      return (
        id.startsWith("filter-group-end:") ||
        id.startsWith("filter-group-start:") ||
        id.startsWith("filter-group-header:") ||
        id.startsWith("filter-cross-group-placeholder:")
      );
    });
    const edgeZoneHits = pointerWithin({
      ...args,
      droppableContainers: edgeZones,
    });
    if (edgeZoneHits.length > 0) return edgeZoneHits;

    const activeDragItem = args.active.data.current?.dragItem as
      | ASTDragItem
      | undefined;

    const sortables = args.droppableContainers.filter((c) => {
      if (!String(c.id).startsWith("filter-sortable:")) return false;
      if (!activeDragItem) return true;
      // When dragging a group, consider every sibling sortable. Groups can
      // be nested into other groups (subject to MAX_FILTER_AST_DEPTH),
      // and "drop into target subgroup" is still reachable by hovering
      // deeper into that subgroup's body where closestCenter picks one of
      // its child sortables. With the old kind filter, a top-level subgroup
      // whose siblings are all conditions had no matching sortable at all,
      // so the strategy never got an `over` and couldn't slide neighbours.
      if (activeDragItem.kind === "group") return true;
      // When dragging a condition, restrict to other conditions so the
      // closest match doesn't snap onto a sibling subgroup (which the user
      // most likely meant to drop *into*, not *next to*).
      const targetKind = (c.data.current?.dragItem as ASTDragItem | undefined)
        ?.kind;
      return targetKind === activeDragItem.kind;
    });

    return closestCenter({ ...args, droppableContainers: sortables });
  }, []);

  const stopAutoScroll = useCallback(() => {
    if (autoScrollFrameRef.current !== undefined) {
      cancelAnimationFrame(autoScrollFrameRef.current);
      autoScrollFrameRef.current = undefined;
    }
    autoScrollVelocityRef.current = 0;
    autoScrollContainerRef.current = null;
  }, []);

  const startAutoScroll = useCallback(() => {
    if (autoScrollFrameRef.current !== undefined) return;

    const tick = () => {
      const container = autoScrollContainerRef.current;
      const velocity = autoScrollVelocityRef.current;
      if (!container || velocity === 0) {
        autoScrollFrameRef.current = undefined;
        return;
      }
      const maxScrollTop = container.scrollHeight - container.clientHeight;
      const nextScrollTop = Math.max(
        0,
        Math.min(maxScrollTop, container.scrollTop + velocity),
      );
      if (nextScrollTop !== container.scrollTop) {
        container.scrollTop = nextScrollTop;
        autoScrollFrameRef.current = requestAnimationFrame(tick);
        return;
      }
      autoScrollFrameRef.current = undefined;
    };

    autoScrollFrameRef.current = requestAnimationFrame(tick);
  }, []);

  const updateAutoScroll = useCallback(
    (pointerY?: number) => {
      if (isNarrow || pointerY === undefined) {
        stopAutoScroll();
        return;
      }

      const scrollContainer = builderRef.current?.closest(
        ".filter-sidebar-panel",
      ) as HTMLElement | null;

      if (!scrollContainer) {
        stopAutoScroll();
        return;
      }

      const containerRect = scrollContainer.getBoundingClientRect();
      const edgeThreshold = 72;
      const maxVelocity = 22;
      let velocity = 0;

      if (pointerY < containerRect.top + edgeThreshold) {
        velocity =
          -maxVelocity * (1 - (pointerY - containerRect.top) / edgeThreshold);
      } else if (pointerY > containerRect.bottom - edgeThreshold) {
        velocity =
          maxVelocity * (1 - (containerRect.bottom - pointerY) / edgeThreshold);
      }

      const clampedVelocity =
        Math.abs(velocity) < 0.5
          ? 0
          : Math.max(-maxVelocity, Math.min(maxVelocity, velocity));

      if (clampedVelocity === 0) {
        stopAutoScroll();
        return;
      }

      autoScrollContainerRef.current = scrollContainer;
      autoScrollVelocityRef.current = clampedVelocity;
      startAutoScroll();
    },
    [isNarrow, startAutoScroll, stopAutoScroll],
  );

  const handleDragStart = useCallback((event: DragStartEvent) => {
    const dragItem = event.active.data.current?.dragItem as
      | ASTDragItem
      | undefined;
    const pointerY = getEventClientY(event.activatorEvent);
    const itemElement =
      dragItem && builderRef.current
        ? builderRef.current.querySelector<HTMLElement>(
            `[data-filter-node-id="${dragItem.nodeId}"]`,
          )
        : null;
    const scrollContainer = builderRef.current?.closest(
      ".filter-sidebar-panel",
    ) as HTMLElement | null;
    const itemRect = itemElement?.getBoundingClientRect();
    pointerYRef.current = pointerY;
    dragGrabOffsetYRef.current =
      itemRect && pointerY !== undefined
        ? Math.max(0, pointerY - itemRect.top)
        : 0;
    dragScrollContainerRef.current = scrollContainer;
    dragStartScrollTopRef.current = scrollContainer?.scrollTop ?? 0;
    // Expose the dragged item's height as a CSS var so the cross-group drop
    // placeholder matches its size and the target subgroup expands to fit.
    if (itemRect && builderRef.current) {
      builderRef.current.style.setProperty(
        "--filter-drag-height",
        `${itemRect.height}px`,
      );
    }
    setDraggedItem(dragItem);
  }, []);

  const handleDragMove = useCallback(() => {
    updateAutoScroll(pointerYRef.current);
  }, [updateAutoScroll]);

  const handleDragOver = useCallback(
    (event: DragOverEvent) => {
      const dragItem = event.active.data.current?.dragItem as
        | ASTDragItem
        | undefined;
      const overId = event.over ? String(event.over.id) : undefined;

      let next: { groupId: string; index: number } | undefined;

      if (dragItem && overId && root) {
        if (overId.startsWith("filter-cross-group-placeholder:")) {
          const parsed = parsePlaceholderID(overId);
          if (parsed && parsed.groupId !== dragItem.parentGroupId) {
            next = parsed;
          }
        } else if (
          overId.startsWith("filter-group-start:") ||
          overId.startsWith("filter-group-header:")
        ) {
          const targetGroupId = overId.replace(
            /^filter-group-(start|header):/,
            "",
          );
          let resolved: { groupId: string; index: number } | undefined;
          if (targetGroupId === root.id) {
            resolved = { groupId: root.id, index: 0 };
          } else {
            const parentInfo = findParentOf(root, targetGroupId);
            if (parentInfo) {
              resolved = {
                groupId: parentInfo.parent.id,
                index: parentInfo.index,
              };
            }
          }
          if (resolved && resolved.groupId !== dragItem.parentGroupId) {
            next = resolved;
          }
        } else if (!overId.startsWith("filter-group-end:")) {
          const overSortable = event.over?.data.current?.sortable as
            | { containerId: string; index: number }
            | undefined;
          if (
            overSortable &&
            overSortable.containerId !== dragItem.parentGroupId
          ) {
            const overRect = event.over?.rect;
            const isBefore =
              overRect && pointerYRef.current !== undefined
                ? pointerYRef.current < overRect.top + overRect.height / 2
                : true;
            next = {
              groupId: overSortable.containerId,
              index: isBefore ? overSortable.index : overSortable.index + 1,
            };
          }
        }
      }

      // Suppress the placeholder when a dragged group's drop would be
      // refused by moveASTNode — either because the target is the dragged
      // group itself or one of its descendants (cycle), or because the
      // drop would push the tree past MAX_FILTER_AST_DEPTH. Without this
      // the user sees the placeholder appear and assumes the drop will
      // land, then releases into a silent no-op.
      if (next && dragItem?.kind === "group" && root) {
        const draggedNode = findASTNodePreview(root, dragItem.nodeId);
        if (draggedNode && draggedNode.kind === "group") {
          if (
            containsGroupId(draggedNode, next.groupId) ||
            wouldExceedDepth(root, draggedNode, next.groupId)
          ) {
            next = undefined;
          }
        }
      }

      const current = crossGroupInsertRef.current;
      if (current?.groupId === next?.groupId && current?.index === next?.index)
        return;
      crossGroupInsertRef.current = next;
      setCrossGroupInsert(next);
    },
    [root],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const dragItem = event.active.data.current?.dragItem as
        | ASTDragItem
        | undefined;
      const overId = event.over ? String(event.over.id) : undefined;

      if (root && dragItem && overId) {
        let dropTarget: ASTDropTarget | undefined;

        if (overId.startsWith("filter-group-end:")) {
          const targetGroupId = overId.replace("filter-group-end:", "");
          const targetGroup = findGroupById(root, targetGroupId);
          if (targetGroup) {
            dropTarget = {
              groupId: targetGroupId,
              index: targetGroup.children.length,
            };
          }
        } else if (overId.startsWith("filter-cross-group-placeholder:")) {
          const parsed = parsePlaceholderID(overId);
          if (parsed && parsed.groupId !== dragItem.parentGroupId) {
            dropTarget = parsed;
          }
        } else if (
          overId.startsWith("filter-group-start:") ||
          overId.startsWith("filter-group-header:")
        ) {
          const targetGroupId = overId.replace(
            /^filter-group-(start|header):/,
            "",
          );
          let resolved: ASTDropTarget | undefined;
          if (targetGroupId === root.id) {
            resolved = { groupId: root.id, index: 0 };
          } else {
            const parentInfo = findParentOf(root, targetGroupId);
            if (parentInfo) {
              resolved = {
                groupId: parentInfo.parent.id,
                index: parentInfo.index,
              };
            }
          }
          // Same-parent attempts here have no visual placeholder during
          // the drag, so completing the drop would be a surprise reorder.
          if (resolved && resolved.groupId !== dragItem.parentGroupId) {
            dropTarget = resolved;
          }
        } else {
          const overSortable = event.over?.data.current?.sortable as
            | { containerId: string; index: number }
            | undefined;
          if (overSortable) {
            if (dragItem.parentGroupId !== overSortable.containerId) {
              dropTarget = crossGroupInsertRef.current ?? {
                groupId: overSortable.containerId,
                index: overSortable.index,
              };
            } else {
              const isBefore = dragItem.index > overSortable.index;
              dropTarget = {
                groupId: overSortable.containerId,
                index: isBefore ? overSortable.index : overSortable.index + 1,
              };
            }
          }
        }

        if (dropTarget) {
          onChange(moveASTNode(mode, root, dragItem, dropTarget));
        }
      }

      crossGroupInsertRef.current = undefined;
      setCrossGroupInsert(undefined);
      pointerYRef.current = undefined;
      dragGrabOffsetYRef.current = 0;
      dragScrollContainerRef.current = null;
      dragStartScrollTopRef.current = 0;
      builderRef.current?.style.removeProperty("--filter-drag-height");
      stopAutoScroll();
      setDraggedItem(undefined);
    },
    [mode, onChange, root, stopAutoScroll],
  );

  const handleDragCancel = useCallback(() => {
    crossGroupInsertRef.current = undefined;
    setCrossGroupInsert(undefined);
    pointerYRef.current = undefined;
    dragGrabOffsetYRef.current = 0;
    dragScrollContainerRef.current = null;
    dragStartScrollTopRef.current = 0;
    builderRef.current?.style.removeProperty("--filter-drag-height");
    stopAutoScroll();
    setDraggedItem(undefined);
  }, [stopAutoScroll]);

  useEffect(() => {
    if (!draggedItem || isNarrow) return undefined;

    const updatePointerPosition = (event: PointerEvent) => {
      pointerYRef.current = event.clientY;
      updateAutoScroll(event.clientY);
    };

    window.addEventListener("pointermove", updatePointerPosition, {
      passive: true,
    });
    return () =>
      window.removeEventListener("pointermove", updatePointerPosition);
  }, [draggedItem, isNarrow, updateAutoScroll]);

  useEffect(() => {
    if (!draggedItem || isNarrow || typeof document === "undefined") {
      return undefined;
    }
    document.body.classList.add("filter-dragging");
    return () => document.body.classList.remove("filter-dragging");
  }, [draggedItem, isNarrow]);

  useEffect(
    () => () => {
      pointerYRef.current = undefined;
      dragGrabOffsetYRef.current = 0;
      dragScrollContainerRef.current = null;
      dragStartScrollTopRef.current = 0;
      stopAutoScroll();
    },
    [stopAutoScroll],
  );

  if (!root) {
    return (
      // `filter-builder` matches the populated-state wrapper below so the
      // mobile horizontal-padding rule in globals.css applies in both
      // states — without it, the locked-conditions/saved-filters bar
      // jumps wider when the user clears the last filter condition.
      <div className="filter-builder flex flex-col gap-3">
        {lockedRoot && <LockedConditions lockedRoot={lockedRoot} />}
        <SavedFilterBar
          filter={filter}
          setFilter={setFilter}
          currentSavedFilterName={currentSavedFilterName}
          onCurrentSavedFilterChange={onCurrentSavedFilterChange}
          view={view}
        />
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Filter />
            </EmptyMedia>
            <EmptyTitle>
              <FormattedMessage
                id="search_filter.empty_title"
                defaultMessage="No filters yet"
              />
            </EmptyTitle>
            <EmptyDescription>
              <FormattedMessage
                id="search_filter.empty_body"
                defaultMessage="Start by adding a condition. It will be placed in a root group automatically."
              />
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button
              variant="default"
              onClick={() => onChange(createASTGroup(mode))}
            >
              <FormattedMessage
                id="search_filter.add_first_condition"
                defaultMessage="Add first condition"
              />
            </Button>
          </EmptyContent>
        </Empty>
      </div>
    );
  }

  return (
    <DndContext
      sensors={isNarrow ? undefined : sensors}
      autoScroll={false}
      collisionDetection={collisionDetection}
      onDragStart={handleDragStart}
      onDragMove={handleDragMove}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
      onDragCancel={handleDragCancel}
    >
      <div
        ref={builderRef}
        className={`filter-builder flex flex-col gap-3${
          draggedItem ? " filter-dragging" : ""
        }`}
      >
        {lockedRoot && <LockedConditions lockedRoot={lockedRoot} />}
        <SavedFilterBar
          filter={filter}
          setFilter={setFilter}
          currentSavedFilterName={currentSavedFilterName}
          onCurrentSavedFilterChange={onCurrentSavedFilterChange}
          view={view}
        />
        <GroupEditor
          mode={mode}
          draggedItem={draggedItem}
          crossGroupInsert={crossGroupInsert}
          isNarrow={isNarrow}
          node={root}
          pinnedFields={pinnedFields}
          onChange={onChange}
          onRemove={() => onChange(undefined)}
          onTogglePinnedField={togglePinnedField}
        />
        {pasteError && (
          <div className="text-sm text-destructive">{pasteError}</div>
        )}
        <div className="flex items-center justify-end gap-2 px-3 pt-1 pb-3">
          {root && (
            <Button
              variant="outline"
              size="icon"
              title="Copy filter as JSON"
              onClick={() => {
                navigator.clipboard.writeText(
                  JSON.stringify(encodeFilterASTNode(root), null, 2),
                );
                setCopied(true);
                window.clearTimeout(copiedTimerRef.current);
                copiedTimerRef.current = window.setTimeout(
                  () => setCopied(false),
                  1500,
                );
              }}
            >
              {copied ? <Check size={14} /> : <Copy size={14} />}
            </Button>
          )}
          <Button variant="destructive" onClick={() => onChange(undefined)}>
            <FormattedMessage
              id="actions.clear_all"
              defaultMessage="Clear All"
            />
          </Button>
        </div>
      </div>
      {createPortal(
        <DragOverlay dropAnimation={null} modifiers={dragOverlayModifiers}>
          {draggedItem ? (
            <FilterDragPreview
              mode={mode}
              draggedItem={draggedItem}
              root={root}
            />
          ) : null}
        </DragOverlay>,
        document.body,
      )}
    </DndContext>
  );
};
