import React, { useCallback, useEffect, useMemo, useState } from "react";
import { useIntl } from "react-intl";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { ChevronDown, ChevronUp, Trash2 } from "lucide-react";
import { Button } from "src/components/ui/button";
import { Card, CardContent } from "src/components/ui/card";
import { Separator } from "src/components/ui/separator";
import { NumberInput } from "./number-input";
import {
  PinButton,
  PinnableComboBox,
} from "src/components/ui/pinnable-combo-box";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import {
  type FilterASTConditionNode,
  astCriterionOptions,
  createASTCondition,
} from "src/models/list-filter/filter-ast";
import type { FilterMode } from "src/core/generated-graphql";
import { CriterionEditor } from "./criterion-editor";
import type { CriterionType } from "src/models/list-filter/types";
import { ModifierSelect } from "./modifier-select";
import {
  type ASTDragItem,
  getDepthClass,
  getPinnedFieldsKey,
  makeSortableID,
} from "./filter-builder-types";

// ── Pinned fields hook ────────────────────────────────────────────────────────

function readPinnedFields(mode: FilterMode): CriterionType[] {
  if (typeof window === "undefined") return [];
  try {
    const rawValue = window.localStorage.getItem(getPinnedFieldsKey(mode));
    if (!rawValue) return [];
    const parsed = JSON.parse(rawValue);
    return Array.isArray(parsed)
      ? parsed.filter(
          (value): value is CriterionType => typeof value === "string",
        )
      : [];
  } catch {
    return [];
  }
}

export function usePinnedFields(mode: FilterMode) {
  const [pinnedFields, setPinnedFields] = useState<CriterionType[]>(() =>
    readPinnedFields(mode),
  );

  useEffect(() => {
    if (typeof window === "undefined") return;
    window.localStorage.setItem(
      getPinnedFieldsKey(mode),
      JSON.stringify(pinnedFields),
    );
  }, [mode, pinnedFields]);

  const togglePinnedField = useCallback((field: CriterionType) => {
    setPinnedFields((current) =>
      current.includes(field)
        ? current.filter((value) => value !== field)
        : [...current, field],
    );
  }, []);

  return { pinnedFields, togglePinnedField };
}

// ── Criterion render-prop helpers ─────────────────────────────────────────────

type HierarchyMode =
  | "exact"
  | "ancestors"
  | "descendants"
  | "ancestors_descendants";

const hierarchyModeMessageIDs: Record<HierarchyMode, string> = {
  exact: "studio_tag_hierarchy_mode.exact",
  ancestors: "studio_tag_hierarchy_mode.ancestors",
  descendants: "studio_tag_hierarchy_mode.descendants",
  ancestors_descendants: "studio_tag_hierarchy_mode.ancestors_descendants",
};

const hierarchyModeOptions: HierarchyMode[] = [
  "exact",
  "ancestors",
  "descendants",
  "ancestors_descendants",
];

export const HierarchyModeSelect: React.FC<{
  value: HierarchyMode;
  onChange: (mode: HierarchyMode) => void;
}> = ({ value, onChange }) => {
  const intl = useIntl();
  const options = hierarchyModeOptions.map((m) => ({
    value: m,
    label: intl.formatMessage({ id: hierarchyModeMessageIDs[m] }),
  }));
  return (
    <Select
      value={value}
      onValueChange={(v) => onChange(v as HierarchyMode)}
      items={options}
    >
      <SelectTrigger className="w-full">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
};

export { NumberInput } from "./number-input";

// ── Field combo box ───────────────────────────────────────────────────────────

const FieldComboBox: React.FC<{
  value: CriterionType;
  options: ReturnType<typeof astCriterionOptions>;
  pinnedFields: CriterionType[];
  onChange: (value: CriterionType) => void;
  onTogglePinned: (value: CriterionType) => void;
}> = ({ value, options, pinnedFields, onChange, onTogglePinned }) => {
  const intl = useIntl();

  const sortedOptions = useMemo(
    () =>
      options.map((option) => ({
        value: option.type,
        label: intl.formatMessage({ id: option.messageID }),
      })),
    [intl, options],
  );

  const currentLabel =
    sortedOptions.find((o) => o.value === value)?.label ?? "";

  return (
    <PinnableComboBox
      selectedValue={value}
      currentLabel={currentLabel}
      options={sortedOptions}
      searchPlaceholder="Search fields…"
      pinnedValues={pinnedFields}
      pinnedSectionLabel="Pinned"
      allSectionLabel="All fields"
      triggerClassName="w-full"
      onSelect={(v) => onChange(v as CriterionType)}
      renderItemAddon={(v, isPinned) => (
        <PinButton
          pinned={isPinned}
          pinnedTitle="Unpin field"
          unpinnedTitle="Pin field"
          onToggle={() => onTogglePinned(v as CriterionType)}
        />
      )}
    />
  );
};

// ── Drag handle (also used by GroupEditor) ────────────────────────────────────

export const FilterDragHandle: React.FC<{
  attributes: Pick<ReturnType<typeof useSortable>, "attributes">["attributes"];
  listeners: Pick<ReturnType<typeof useSortable>, "listeners">["listeners"];
  setActivatorNodeRef: Pick<
    ReturnType<typeof useSortable>,
    "setActivatorNodeRef"
  >["setActivatorNodeRef"];
  isDragging: boolean;
  label: string;
}> = ({ attributes, listeners, setActivatorNodeRef, isDragging, label }) => (
  <span
    ref={setActivatorNodeRef}
    className="filter-drag-rail"
    title={label}
    aria-label={label}
    data-dragging={isDragging || undefined}
    {...attributes}
    {...listeners}
  >
    <span className="text-[10px] leading-[0.7] select-none" aria-hidden="true">
      ⋮⋮
    </span>
  </span>
);

// ── ConditionEditor ───────────────────────────────────────────────────────────

export type ConditionEditorProps = {
  mode: FilterMode;
  depth: number;
  isDragging: boolean;
  isNarrow: boolean;
  node: FilterASTConditionNode;
  parentGroupId: string;
  index: number;
  pinnedFields: CriterionType[];
  onChange: (node: FilterASTConditionNode) => void;
  onRemove: () => void;
  onTogglePinnedField: (field: CriterionType) => void;
  onMoveUp?: () => void;
  onMoveDown?: () => void;
  canMoveUp?: boolean;
  canMoveDown?: boolean;
};

const ConditionEditorComponent: React.FC<ConditionEditorProps> = ({
  mode,
  depth,
  isDragging,
  isNarrow,
  node,
  parentGroupId,
  index,
  pinnedFields,
  onChange,
  onRemove,
  onTogglePinnedField,
  onMoveUp,
  onMoveDown,
  canMoveUp = false,
  canMoveDown = false,
}) => {
  const intl = useIntl();
  const criterionOptions = astCriterionOptions(mode);

  const onChangeField = useCallback(
    (field: CriterionType) => {
      onChange(createASTCondition(mode, field));
    },
    [mode, onChange],
  );

  const dragItem: ASTDragItem = {
    index,
    kind: "condition",
    nodeId: node.id,
    parentGroupId,
  };
  const {
    attributes: dragAttributes,
    listeners: dragListeners,
    setNodeRef: setDragNodeRef,
    setActivatorNodeRef,
    isDragging: isDragHandleDragging,
    transform,
    // dnd-kit's `transition` is intentionally `transform 0ms linear` for the
    // first transform change of each item per drag, so the first sibling
    // shift snaps instead of sliding. We ignore it here and let a CSS rule
    // scoped to `.filter-builder.filter-dragging` provide a uniform
    // transition for all items throughout the drag (and remove it on drop).
  } = useSortable({
    id: makeSortableID(node.id),
    data: { dragItem },
    disabled: isNarrow,
  });

  const shellClass = `filter-condition-shell ${getDepthClass(depth)}${
    isDragging ? " filter-item-dragging" : ""
  }`;

  const rowClass = "flex flex-col gap-2 mt-2";

  return (
    <div
      ref={setDragNodeRef}
      className={shellClass}
      data-filter-node-id={node.id}
      style={{ transform: CSS.Transform.toString(transform) }}
    >
      {!isNarrow ? (
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
      ) : null}
      <Card className="filter-condition-card flex-1 min-w-0" size="sm">
        <CardContent className="flex flex-row items-start">
          <div className="flex flex-col flex-1 min-w-0">
            {/* topline: field selector + toggle + inline delete */}
            <div className="flex items-center gap-2 flex-wrap">
              <div className="flex-1 min-w-0">
                <FieldComboBox
                  value={node.field}
                  options={criterionOptions}
                  pinnedFields={pinnedFields}
                  onChange={onChangeField}
                  onTogglePinned={onTogglePinnedField}
                />
              </div>
              {!isNarrow ? (
                <Button
                  variant="destructive"
                  size="default"
                  className="filter-inline-delete"
                  onClick={onRemove}
                >
                  Delete
                </Button>
              ) : null}
            </div>
            {/* criterion row */}
            <div className={rowClass}>
              <CriterionEditor
                criterion={node.criterion}
                setCriterion={(criterion) => onChange({ ...node, criterion })}
                mode={mode}
                renderModifierSelect={(options, val, onChanged) => (
                  <ModifierSelect
                    options={options}
                    value={val}
                    onChanged={onChanged}
                  />
                )}
                renderHierarchyModeSelect={(val, onChanged) => (
                  <HierarchyModeSelect value={val} onChange={onChanged} />
                )}
                renderSimpleSelect={(options, val, onChanged) => (
                  <Select
                    value={val}
                    onValueChange={(v) => v !== null && onChanged(v)}
                    items={options}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent visibleItems={7}>
                      {options.map((o) => (
                        <SelectItem key={o.value} value={o.value}>
                          {o.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
                renderNumberInput={(val, onChanged, min) => (
                  <NumberInput value={val} onChange={onChanged} min={min} />
                )}
              />
            </div>
          </div>
          {isNarrow ? (
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
    </div>
  );
};

// Use default shallow comparison so that onMoveUp/onMoveDown (which capture
// index and sibling-type state at render time) are always current when the
// parent group re-renders after a sibling is reorganised.
export const ConditionEditor = React.memo(ConditionEditorComponent);
