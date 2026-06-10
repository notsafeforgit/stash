import {
  createASTCondition,
  type FilterASTGroupNode,
  type FilterASTNode,
} from "src/models/list-filter/filter-ast";
import type { FilterMode } from "src/core/generated-graphql";
import type { CriterionType } from "src/models/list-filter/types";

export type ASTDragItem = {
  index: number;
  kind: FilterASTNode["kind"];
  nodeId: string;
  parentGroupId: string;
};

export type ASTDropTarget = {
  groupId: string;
  index: number;
};

export const MAX_FILTER_AST_DEPTH = 4;

export function makeSortableID(nodeId: string) {
  return `filter-sortable:${nodeId}`;
}

export function makeGroupEndID(groupId: string) {
  return `filter-group-end:${groupId}`;
}

export function makeGroupStartID(groupId: string) {
  return `filter-group-start:${groupId}`;
}

export function makeGroupHeaderID(groupId: string) {
  return `filter-group-header:${groupId}`;
}

export function makePlaceholderID(groupId: string, index: number) {
  return `filter-cross-group-placeholder:${groupId}:${index}`;
}

export function parsePlaceholderID(
  id: string,
): { groupId: string; index: number } | undefined {
  const match = id.match(/^filter-cross-group-placeholder:(.+):(-?\d+)$/);
  if (!match) return undefined;
  return { groupId: match[1], index: parseInt(match[2], 10) };
}

/**
 * Returns the parent group of `groupId` and the index where it sits, or
 * undefined if `groupId` is the root of the tree.
 */
export function findParentOf(
  root: FilterASTGroupNode,
  groupId: string,
): { parent: FilterASTGroupNode; index: number } | undefined {
  for (let i = 0; i < root.children.length; i++) {
    const child = root.children[i];
    if (child.id === groupId) return { parent: root, index: i };
    if (child.kind === "group") {
      const found = findParentOf(child, groupId);
      if (found) return found;
    }
  }
  return undefined;
}

export function getDepthClass(depth: number) {
  return `filter-depth-${Math.min(depth, MAX_FILTER_AST_DEPTH)}`;
}

// Returns the max number of additional group levels nested within `node`.
// 0 means the node has no nested group children.
export function maxGroupNestingDepth(node: FilterASTNode): number {
  if (node.kind === "condition") return 0;
  const groupChildren = node.children.filter((c) => c.kind === "group");
  if (groupChildren.length === 0) return 0;
  return 1 + Math.max(...groupChildren.map(maxGroupNestingDepth));
}

export function findASTNodePreview(
  group: FilterASTGroupNode,
  nodeId: string,
): FilterASTNode | undefined {
  if (group.id === nodeId) return group;
  for (const child of group.children) {
    if (child.id === nodeId) return child;
    if (child.kind === "group") {
      const found = findASTNodePreview(child, nodeId);
      if (found) return found;
    }
  }
  return undefined;
}

export function getEventClientY(event: Event): number | undefined {
  if (event instanceof MouseEvent || event instanceof PointerEvent) {
    return event.clientY;
  }
  if (typeof TouchEvent !== "undefined" && event instanceof TouchEvent) {
    if (event.touches[0]) return event.touches[0].clientY;
    if (event.changedTouches[0]) return event.changedTouches[0].clientY;
  }
  return undefined;
}

export function updateChildAtIndex<T>(items: T[], index: number, item: T) {
  return items.map((value, idx) => (idx === index ? item : value));
}

export function moveItem<T>(items: T[], fromIndex: number, toIndex: number) {
  if (
    fromIndex === toIndex ||
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= items.length ||
    toIndex >= items.length
  ) {
    return items;
  }
  const next = [...items];
  const [item] = next.splice(fromIndex, 1);
  next.splice(toIndex, 0, item);
  return next;
}

export function findGroupById(
  group: FilterASTGroupNode,
  groupId: string,
): FilterASTGroupNode | undefined {
  if (group.id === groupId) return group;
  for (const child of group.children) {
    if (child.kind === "group") {
      const nested = findGroupById(child, groupId);
      if (nested) return nested;
    }
  }
  return undefined;
}

/**
 * Returns the depth at which `groupId` lives in the tree (root = 0), or
 * undefined if the id isn't a group in the tree.
 */
export function findGroupDepth(
  root: FilterASTGroupNode,
  groupId: string,
  currentDepth = 0,
): number | undefined {
  if (root.id === groupId) return currentDepth;
  for (const child of root.children) {
    if (child.kind === "group") {
      const found = findGroupDepth(child, groupId, currentDepth + 1);
      if (found !== undefined) return found;
    }
  }
  return undefined;
}

/**
 * Whether dropping `draggedNode` into `targetGroupId` would push the tree
 * past MAX_FILTER_AST_DEPTH. Conditions never deepen the tree (always
 * allowed); a group's deepest descendant lands at
 * `targetDepth + 1 + maxGroupNestingDepth(draggedNode)`.
 */
export function wouldExceedDepth(
  root: FilterASTGroupNode,
  draggedNode: FilterASTNode,
  targetGroupId: string,
): boolean {
  if (draggedNode.kind !== "group") return false;
  const targetDepth = findGroupDepth(root, targetGroupId);
  if (targetDepth === undefined) return true;
  const resultingDepth = targetDepth + 1 + maxGroupNestingDepth(draggedNode);
  return resultingDepth > MAX_FILTER_AST_DEPTH;
}

export function updateGroupById(
  group: FilterASTGroupNode,
  groupId: string,
  updater: (current: FilterASTGroupNode) => FilterASTGroupNode,
): FilterASTGroupNode {
  if (group.id === groupId) return updater(group);
  return {
    ...group,
    children: group.children.map((child) =>
      child.kind === "group" ? updateGroupById(child, groupId, updater) : child,
    ),
  };
}

export function containsGroupId(node: FilterASTNode, groupId: string): boolean {
  if (node.kind !== "group") return false;
  if (node.id === groupId) return true;
  return node.children.some((child) => containsGroupId(child, groupId));
}

export function pruneEmptyGroups(
  mode: FilterMode,
  group: FilterASTGroupNode,
  isRoot = true,
): FilterASTGroupNode | undefined {
  const children = group.children
    .map((child) => {
      if (child.kind !== "group") return child;
      return pruneEmptyGroups(mode, child, false);
    })
    .filter(Boolean) as FilterASTNode[];

  if (!children.length && !isRoot) return undefined;

  return {
    ...group,
    children: children.length ? children : [createASTCondition(mode)],
  };
}

export function moveASTNode(
  mode: FilterMode,
  root: FilterASTGroupNode,
  dragItem: ASTDragItem,
  target: ASTDropTarget,
): FilterASTGroupNode {
  const sourceGroup = findGroupById(root, dragItem.parentGroupId);
  if (!sourceGroup) return root;

  const draggedNode = sourceGroup.children[dragItem.index];
  if (!draggedNode) return root;

  if (
    draggedNode.kind === "group" &&
    containsGroupId(draggedNode, target.groupId)
  ) {
    return root;
  }

  // Same-parent reorders don't deepen the tree, so we only depth-check
  // cross-parent moves of group nodes.
  if (
    dragItem.parentGroupId !== target.groupId &&
    wouldExceedDepth(root, draggedNode, target.groupId)
  ) {
    return root;
  }

  if (dragItem.parentGroupId === target.groupId) {
    return updateGroupById(root, dragItem.parentGroupId, (group) => {
      const children = [...group.children];
      const [movedNode] = children.splice(dragItem.index, 1);
      let insertIndex = target.index;
      if (dragItem.index < target.index) insertIndex -= 1;
      children.splice(insertIndex, 0, movedNode);
      return { ...group, children };
    });
  }

  const withoutSource = updateGroupById(
    root,
    dragItem.parentGroupId,
    (group) => ({
      ...group,
      children: group.children.filter((_, index) => index !== dragItem.index),
    }),
  );

  const prunedSourceTree = pruneEmptyGroups(mode, withoutSource);
  if (!prunedSourceTree) return root;

  return updateGroupById(prunedSourceTree, target.groupId, (group) => {
    const children = [...group.children];
    children.splice(Math.min(target.index, children.length), 0, draggedNode);
    return { ...group, children };
  });
}

export const stackedTextFields = new Set<CriterionType>([
  "title",
  "details",
  "code",
  "director",
  "path",
  "folder",
  "parent_folder",
  "url",
  "oshash",
  "checksum",
  "captions",
]);

export const stackedValueFields = new Set<CriterionType>([
  "groups",
  "galleries",
]);

export function getPinnedFieldsKey(mode: FilterMode) {
  return `filter-builder-pinned-fields:${mode}`;
}

export function getPinnedSavedFiltersKey(mode: FilterMode) {
  return `filter-builder-pinned-saved-filters:${mode}`;
}
