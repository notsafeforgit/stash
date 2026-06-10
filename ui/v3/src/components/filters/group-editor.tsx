import React, { useCallback, useRef } from "react";
import { useIntl } from "react-intl";
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useDroppable } from "@dnd-kit/core";
import { ChevronDown, ChevronUp, Trash2 } from "lucide-react";
import { Button } from "src/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
} from "src/components/ui/card";
import { Separator } from "src/components/ui/separator";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import {
  type FilterASTGroupNode,
  type FilterASTNode,
  createASTCondition,
  createASTGroup,
} from "src/models/list-filter/filter-ast";
import {
  FilterGroupOperator,
  type FilterMode,
} from "src/core/generated-graphql";
import type { CriterionType } from "src/models/list-filter/types";
import {
  type ASTDragItem,
  MAX_FILTER_AST_DEPTH,
  getDepthClass,
  makeGroupEndID,
  makeGroupHeaderID,
  makeGroupStartID,
  makePlaceholderID,
  makeSortableID,
  maxGroupNestingDepth,
  moveItem,
  updateChildAtIndex,
} from "./filter-builder-types";
import { ConditionEditor, FilterDragHandle } from "./condition-editor";

// ── Group operator select ─────────────────────────────────────────────────────

const GroupOperatorSelect: React.FC<{
  value: FilterGroupOperator;
  onChange: (value: FilterGroupOperator) => void;
}> = ({ value, onChange }) => {
  const intl = useIntl();
  const operatorOptions = [
    {
      value: FilterGroupOperator.And,
      label: intl.formatMessage({
        id: FilterGroupOperator.And,
        defaultMessage: FilterGroupOperator.And,
      }),
    },
    {
      value: FilterGroupOperator.Or,
      label: intl.formatMessage({
        id: FilterGroupOperator.Or,
        defaultMessage: FilterGroupOperator.Or,
      }),
    },
  ];
  const currentLabel =
    operatorOptions.find((o) => o.value === value)?.label ?? value;
  return (
    <Select
      value={value}
      onValueChange={(v) => onChange(v as FilterGroupOperator)}
    >
      <SelectTrigger className="w-full">
        <SelectValue>{currentLabel}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        {operatorOptions.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
};

// ── Group start/end drop zones ────────────────────────────────────────────────

const GroupEndZone: React.FC<{
  depth: number;
  enabled: boolean;
  groupId: string;
}> = ({ depth, enabled, groupId }) => {
  const { setNodeRef, isOver } = useDroppable({
    id: makeGroupEndID(groupId),
    disabled: !enabled,
  });

  if (!enabled) return null;

  return (
    <div
      ref={setNodeRef}
      className={`filter-drop-zone ${getDepthClass(depth)}${
        isOver ? " filter-drop-zone-active" : ""
      }`}
      style={depth > 0 ? { marginLeft: `${depth * 1.1}rem` } : undefined}
    />
  );
};

/**
 * Hit target at the top of every group's body. Activated only during a drag.
 * For a subgroup, hovering this zone (or the group's header) is treated as
 * "escape this group" — the placeholder renders in the *parent* group, before
 * this subgroup, so the user sees the item land outside. For the root group
 * (no parent), it falls through to "drop at root index 0".
 */
const GroupStartZone: React.FC<{
  enabled: boolean;
  groupId: string;
}> = ({ enabled, groupId }) => {
  const { setNodeRef } = useDroppable({
    id: makeGroupStartID(groupId),
    disabled: !enabled,
  });

  if (!enabled) return null;

  return (
    <div
      ref={setNodeRef}
      className="filter-group-start-hit"
      aria-hidden="true"
    />
  );
};

/**
 * Cross-group drop indicator slot. Registered as a droppable so the cursor
 * stays parked on it after the layout shift caused by its insertion (without
 * this, the layout reflow pushes the original drop target out from under the
 * cursor and the over-state oscillates). Resolves to the same (groupId,
 * index) the trigger zone resolved to.
 */
const CrossGroupPlaceholder: React.FC<{
  groupId: string;
  index: number;
  draggedKind?: ASTDragItem["kind"];
}> = ({ groupId, index, draggedKind }) => {
  const { setNodeRef } = useDroppable({
    id: makePlaceholderID(groupId, index),
  });
  return (
    <div
      ref={setNodeRef}
      className={`filter-cross-group-placeholder filter-cross-group-placeholder-${draggedKind ?? "condition"}`}
      aria-hidden="true"
    />
  );
};

// ── GroupEditor ───────────────────────────────────────────────────────────────

export type GroupEditorProps = {
  mode: FilterMode;
  draggedItem?: ASTDragItem;
  crossGroupInsert?: { groupId: string; index: number };
  isNarrow: boolean;
  node: FilterASTGroupNode;
  pinnedFields: CriterionType[];
  onChange: (node: FilterASTGroupNode) => void;
  onRemove?: () => void;
  onTogglePinnedField: (field: CriterionType) => void;
  onMoveUp?: () => void;
  onMoveDown?: () => void;
  canMoveUp?: boolean;
  canMoveDown?: boolean;
  parentGroupId?: string;
  itemIndex?: number;
  depth?: number;
  hasSuccessorInTree?: boolean;
  hasPredecessorInTree?: boolean;
  onMoveChildDownOut?: (child: FilterASTNode) => void;
  onMoveChildUpOut?: (child: FilterASTNode) => void;
};

const GroupEditorComponent: React.FC<GroupEditorProps> = ({
  mode,
  draggedItem,
  crossGroupInsert,
  isNarrow,
  node,
  pinnedFields,
  onChange,
  onRemove,
  onTogglePinnedField,
  onMoveUp,
  onMoveDown,
  canMoveUp = false,
  canMoveDown = false,
  parentGroupId,
  itemIndex,
  depth = 0,
  hasSuccessorInTree = false,
  hasPredecessorInTree = false,
  onMoveChildDownOut,
  onMoveChildUpOut,
}) => {
  const intl = useIntl();

  const nodeRef = useRef(node);
  nodeRef.current = node;
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const onRemoveRef = useRef(onRemove);
  onRemoveRef.current = onRemove;
  const onMoveChildDownOutRef = useRef(onMoveChildDownOut);
  onMoveChildDownOutRef.current = onMoveChildDownOut;
  const onMoveChildUpOutRef = useRef(onMoveChildUpOut);
  onMoveChildUpOutRef.current = onMoveChildUpOut;

  const updateChild = useCallback((index: number, child: FilterASTNode) => {
    const n = nodeRef.current;
    onChangeRef.current({
      ...n,
      children: updateChildAtIndex(n.children, index, child),
    });
  }, []);

  const removeChild = useCallback(
    (index: number) => {
      const n = nodeRef.current;
      const children = n.children.filter(
        (_, childIndex) => childIndex !== index,
      );
      if (!children.length && onRemoveRef.current) {
        onRemoveRef.current();
        return;
      }
      onChangeRef.current({
        ...n,
        children: children.length ? children : [createASTCondition(mode)],
      });
    },
    [mode],
  );

  const moveChild = useCallback((fromIndex: number, toIndex: number) => {
    const n = nodeRef.current;
    onChangeRef.current({
      ...n,
      children: moveItem(n.children, fromIndex, toIndex),
    });
  }, []);

  const moveConditionIntoNextGroup = useCallback((condIndex: number) => {
    const n = nodeRef.current;
    const cond = n.children[condIndex];
    const nextGroup = n.children[condIndex + 1] as FilterASTGroupNode;
    const updatedGroup: FilterASTGroupNode = {
      ...nextGroup,
      children: [cond, ...nextGroup.children],
    };
    const newChildren = [...n.children];
    newChildren.splice(condIndex, 1);
    newChildren[condIndex] = updatedGroup;
    onChangeRef.current({ ...n, children: newChildren });
  }, []);

  const moveConditionIntoPrevGroup = useCallback((condIndex: number) => {
    const n = nodeRef.current;
    const cond = n.children[condIndex];
    const prevGroup = n.children[condIndex - 1] as FilterASTGroupNode;
    const updatedGroup: FilterASTGroupNode = {
      ...prevGroup,
      children: [...prevGroup.children, cond],
    };
    const newChildren = [...n.children];
    newChildren.splice(condIndex, 1);
    newChildren[condIndex - 1] = updatedGroup;
    onChangeRef.current({ ...n, children: newChildren });
  }, []);

  const moveGroupIntoNextGroup = useCallback((groupIndex: number) => {
    const n = nodeRef.current;
    const movingGroup = n.children[groupIndex];
    const nextGroup = n.children[groupIndex + 1] as FilterASTGroupNode;
    const updatedGroup: FilterASTGroupNode = {
      ...nextGroup,
      children: [movingGroup, ...nextGroup.children],
    };
    const newChildren = [...n.children];
    newChildren.splice(groupIndex, 1);
    newChildren[groupIndex] = updatedGroup;
    onChangeRef.current({ ...n, children: newChildren });
  }, []);

  const moveGroupIntoPrevGroup = useCallback((groupIndex: number) => {
    const n = nodeRef.current;
    const movingGroup = n.children[groupIndex];
    const prevGroup = n.children[groupIndex - 1] as FilterASTGroupNode;
    const updatedGroup: FilterASTGroupNode = {
      ...prevGroup,
      children: [...prevGroup.children, movingGroup],
    };
    const newChildren = [...n.children];
    newChildren.splice(groupIndex, 1);
    newChildren[groupIndex - 1] = updatedGroup;
    onChangeRef.current({ ...n, children: newChildren });
  }, []);

  const renderChild = useCallback(
    (child: FilterASTNode, index: number) => {
      const showInsertBefore =
        crossGroupInsert?.groupId === node.id &&
        crossGroupInsert.index === index;
      const insertIndicator = showInsertBefore ? (
        <CrossGroupPlaceholder
          key={`insert-before-${index}`}
          groupId={node.id}
          index={index}
          draggedKind={draggedItem?.kind}
        />
      ) : null;

      if (child.kind === "condition") {
        const isFirst = index === 0;
        const isLast = index === node.children.length - 1;
        const prevSiblingIsGroup =
          index > 0 && node.children[index - 1].kind === "group";
        const nextSiblingIsGroup =
          index < node.children.length - 1 &&
          node.children[index + 1].kind === "group";

        const handleMoveDown = () => {
          if (!isLast) {
            if (nextSiblingIsGroup) moveConditionIntoNextGroup(index);
            else moveChild(index, index + 1);
          } else {
            onMoveChildDownOutRef.current?.(child);
          }
        };

        const handleMoveUp = () => {
          if (!isFirst) {
            if (prevSiblingIsGroup) moveConditionIntoPrevGroup(index);
            else moveChild(index, index - 1);
          } else {
            onMoveChildUpOutRef.current?.(child);
          }
        };

        return (
          <React.Fragment key={child.id}>
            {insertIndicator}
            <ConditionEditor
              mode={mode}
              depth={depth}
              isDragging={draggedItem?.nodeId === child.id}
              isNarrow={isNarrow}
              node={child}
              parentGroupId={node.id}
              index={index}
              pinnedFields={pinnedFields}
              onChange={(next) => updateChild(index, next)}
              onRemove={() => removeChild(index)}
              onTogglePinnedField={onTogglePinnedField}
              onMoveUp={handleMoveUp}
              onMoveDown={handleMoveDown}
              canMoveUp={!isFirst || hasPredecessorInTree}
              canMoveDown={!isLast || hasSuccessorInTree}
            />
          </React.Fragment>
        );
      }

      // Child group
      const childHasSuccessorInTree = true;
      const childHasPredecessorInTree = true;
      const isFirst = index === 0;
      const isLast = index === node.children.length - 1;
      const prevSiblingIsGroup =
        index > 0 && node.children[index - 1].kind === "group";
      const nextSiblingIsGroup =
        index < node.children.length - 1 &&
        node.children[index + 1].kind === "group";
      const canNestDeeper =
        depth + 2 + maxGroupNestingDepth(child) <= MAX_FILTER_AST_DEPTH;

      const handleGroupMoveDown = () => {
        if (!isLast) {
          if (nextSiblingIsGroup && canNestDeeper)
            moveGroupIntoNextGroup(index);
          else moveChild(index, index + 1);
        } else {
          onMoveChildDownOutRef.current?.(child);
        }
      };

      const handleGroupMoveUp = () => {
        if (!isFirst) {
          if (prevSiblingIsGroup && canNestDeeper)
            moveGroupIntoPrevGroup(index);
          else moveChild(index, index - 1);
        } else {
          onMoveChildUpOutRef.current?.(child);
        }
      };

      const handleChildMoveDownOut = (exitingChild: FilterASTNode) => {
        const n = nodeRef.current;
        const childGroup = n.children[index] as FilterASTGroupNode;
        const updatedGroupChildren = childGroup.children.filter(
          (c) => c.id !== exitingChild.id,
        );
        const newChildren = [...n.children];
        if (updatedGroupChildren.length) {
          newChildren[index] = {
            ...childGroup,
            children: updatedGroupChildren,
          };
          newChildren.splice(index + 1, 0, exitingChild);
        } else {
          newChildren.splice(index, 1, exitingChild);
        }
        onChangeRef.current({ ...n, children: newChildren });
      };

      const handleChildMoveUpOut = (exitingChild: FilterASTNode) => {
        const n = nodeRef.current;
        const childGroup = n.children[index] as FilterASTGroupNode;
        const updatedGroupChildren = childGroup.children.filter(
          (c) => c.id !== exitingChild.id,
        );
        const newChildren = [...n.children];
        if (updatedGroupChildren.length) {
          newChildren[index] = {
            ...childGroup,
            children: updatedGroupChildren,
          };
          newChildren.splice(index, 0, exitingChild);
        } else {
          newChildren.splice(index, 1, exitingChild);
        }
        onChangeRef.current({ ...n, children: newChildren });
      };

      return (
        <React.Fragment key={child.id}>
          {insertIndicator}
          <GroupEditor
            mode={mode}
            draggedItem={draggedItem}
            crossGroupInsert={crossGroupInsert}
            isNarrow={isNarrow}
            node={child}
            pinnedFields={pinnedFields}
            onChange={(next) => updateChild(index, next)}
            onRemove={() => removeChild(index)}
            onTogglePinnedField={onTogglePinnedField}
            onMoveUp={handleGroupMoveUp}
            onMoveDown={handleGroupMoveDown}
            canMoveUp={!isFirst || hasPredecessorInTree}
            canMoveDown={!isLast || hasSuccessorInTree}
            parentGroupId={node.id}
            itemIndex={index}
            depth={depth + 1}
            hasSuccessorInTree={childHasSuccessorInTree}
            hasPredecessorInTree={childHasPredecessorInTree}
            onMoveChildDownOut={handleChildMoveDownOut}
            onMoveChildUpOut={handleChildMoveUpOut}
          />
        </React.Fragment>
      );
    },
    [
      crossGroupInsert,
      depth,
      draggedItem,
      hasSuccessorInTree,
      hasPredecessorInTree,
      isNarrow,
      moveChild,
      moveConditionIntoNextGroup,
      moveConditionIntoPrevGroup,
      moveGroupIntoNextGroup,
      moveGroupIntoPrevGroup,
      node,
      mode,
      onTogglePinnedField,
      pinnedFields,
      removeChild,
      updateChild,
    ],
  );

  const isDragging = draggedItem?.nodeId === node.id;
  const hasDesktopDragRail =
    !isNarrow && parentGroupId !== undefined && itemIndex !== undefined;
  const dragItem: ASTDragItem | undefined = hasDesktopDragRail
    ? {
        index: itemIndex!,
        kind: "group",
        nodeId: node.id,
        parentGroupId: parentGroupId!,
      }
    : undefined;
  const {
    attributes: dragAttributes,
    listeners: dragListeners,
    setNodeRef: setDragNodeRef,
    setActivatorNodeRef,
    isDragging: isDragHandleDragging,
    transform,
    // See condition-editor for why we don't pull dnd-kit's transition.
  } = useSortable({
    id: makeSortableID(node.id),
    data: dragItem ? { dragItem } : {},
    disabled: !hasDesktopDragRail,
  });

  // Header drop target — hovering the header during a drag is the most
  // intuitive way to "drop above this group". For a subgroup, this escapes
  // one level up; for the root, it falls through to "drop at root index 0".
  const { setNodeRef: setHeaderDropRef } = useDroppable({
    id: makeGroupHeaderID(node.id),
    disabled: isNarrow,
  });

  const shellClass = `filter-group-shell ${getDepthClass(depth)}${
    isDragging ? " filter-item-dragging" : ""
  }`;

  const groupCard = (
    <Card className="filter-group-card" size="sm">
      <div ref={setHeaderDropRef} className="filter-group-header-drop">
        <CardHeader className="filter-group-header items-center">
          <div className="min-w-0">
            <GroupOperatorSelect
              value={node.operator}
              onChange={(op) => onChange({ ...node, operator: op })}
            />
          </div>
          {!isNarrow && onRemove ? (
            <CardAction>
              <Button
                variant="destructive"
                size="default"
                className="filter-inline-delete"
                onClick={onRemove}
              >
                Delete
              </Button>
            </CardAction>
          ) : null}
        </CardHeader>
      </div>
      <Separator />
      <CardContent className="filter-group-body flex flex-row p-0">
        <div className="filter-group-body-inner relative flex flex-col gap-2 flex-1 min-w-0 p-3">
          <GroupStartZone enabled={!isNarrow} groupId={node.id} />
          <SortableContext
            id={node.id}
            items={node.children.map((child) => makeSortableID(child.id))}
            strategy={verticalListSortingStrategy}
          >
            {node.children.map((child, index) => renderChild(child, index))}
            {crossGroupInsert?.groupId === node.id &&
            crossGroupInsert.index === node.children.length ? (
              <CrossGroupPlaceholder
                groupId={node.id}
                index={node.children.length}
                draggedKind={draggedItem?.kind}
              />
            ) : null}
          </SortableContext>
          <GroupEndZone
            depth={depth + 1}
            enabled={!isNarrow}
            groupId={node.id}
          />
          <div className="flex flex-wrap items-center gap-2 mt-1">
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                const newCondition = createASTCondition(mode);
                onChange({
                  ...node,
                  children: [...node.children, newCondition],
                });
              }}
            >
              Add condition
            </Button>
            {depth < MAX_FILTER_AST_DEPTH && (
              <Button
                variant="outline"
                size="sm"
                onClick={() =>
                  onChange({
                    ...node,
                    children: [...node.children, createASTGroup(mode)],
                  })
                }
              >
                Add subgroup
              </Button>
            )}
          </div>
        </div>
        {isNarrow && depth > 0 ? (
          <>
            <Separator orientation="vertical" className="mx-2" />
            <div className="filter-mobile-actions">
              <Button variant="destructive" size="icon" onClick={onRemove}>
                <Trash2 size={14} />
              </Button>
              <Button
                variant="outline"
                size="icon"
                onClick={onMoveUp}
                disabled={!canMoveUp}
              >
                <ChevronUp size={14} />
              </Button>
              <Button
                variant="outline"
                size="icon"
                onClick={onMoveDown}
                disabled={!canMoveDown}
              >
                <ChevronDown size={14} />
              </Button>
            </div>
          </>
        ) : null}
      </CardContent>
    </Card>
  );

  return (
    <div
      ref={setDragNodeRef}
      className={shellClass}
      data-filter-node-id={node.id}
      style={{ transform: CSS.Transform.toString(transform) }}
    >
      {hasDesktopDragRail ? (
        <FilterDragHandle
          attributes={dragAttributes}
          listeners={dragListeners}
          setActivatorNodeRef={setActivatorNodeRef}
          isDragging={isDragHandleDragging}
          label={intl.formatMessage({
            id: "actions.drag_to_reorder",
            defaultMessage: "Drag to reorder",
          })}
        />
      ) : (
        <div className="filter-depth-rail" aria-hidden="true" />
      )}
      <div className="flex-1 min-w-0">{groupCard}</div>
    </div>
  );
};

export const GroupEditor = React.memo(
  GroupEditorComponent,
  (prev, next) =>
    prev.isNarrow === next.isNarrow &&
    prev.node === next.node &&
    prev.pinnedFields === next.pinnedFields &&
    prev.canMoveUp === next.canMoveUp &&
    prev.canMoveDown === next.canMoveDown &&
    prev.parentGroupId === next.parentGroupId &&
    prev.itemIndex === next.itemIndex &&
    prev.depth === next.depth &&
    prev.hasSuccessorInTree === next.hasSuccessorInTree &&
    prev.hasPredecessorInTree === next.hasPredecessorInTree &&
    prev.draggedItem?.nodeId === next.draggedItem?.nodeId &&
    prev.crossGroupInsert?.groupId === next.crossGroupInsert?.groupId &&
    prev.crossGroupInsert?.index === next.crossGroupInsert?.index,
);
