import type React from "react";
import { useMemo } from "react";
import { useIntl } from "react-intl";
import {
  astCriterionOptions,
  type FilterASTGroupNode,
} from "src/models/list-filter/filter-ast";
import type { FilterMode } from "src/core/generated-graphql";
import { type ASTDragItem, findASTNodePreview } from "./filter-builder-types";

export const FilterDragPreview: React.FC<{
  mode: FilterMode;
  draggedItem: ASTDragItem;
  root: FilterASTGroupNode;
}> = ({ mode, draggedItem, root }) => {
  const intl = useIntl();
  const criterionOptions = useMemo(() => astCriterionOptions(mode), [mode]);
  const preview = useMemo(
    () => findASTNodePreview(root, draggedItem.nodeId),
    [draggedItem.nodeId, root],
  );

  if (!preview) {
    return (
      <div className="filter-drag-overlay">
        {draggedItem.kind === "group" ? "Group" : "Condition"}
      </div>
    );
  }

  if (preview.kind === "group") {
    const operatorLabel = intl.formatMessage({
      id: preview.operator,
      defaultMessage: preview.operator,
    });
    return (
      <div className="filter-drag-overlay filter-drag-overlay-group">
        <div className="text-[10px] font-semibold uppercase tracking-wider opacity-60">
          Group
        </div>
        <div className="text-sm font-medium">{operatorLabel}</div>
      </div>
    );
  }

  const fieldOption = criterionOptions.find(
    (option) => option.type === preview.field,
  );
  const fieldLabel = fieldOption
    ? intl.formatMessage({ id: fieldOption.messageID })
    : preview.field;

  return (
    <div className="filter-drag-overlay">
      <div className="text-[10px] font-semibold uppercase tracking-wider opacity-60">
        Condition
      </div>
      <div className="text-sm font-medium">{fieldLabel}</div>
    </div>
  );
};
