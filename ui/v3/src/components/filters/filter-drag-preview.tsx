import type React from "react";
import { useMemo } from "react";
import { useIntl } from "react-intl";
import type { FilterASTGroupNode } from "src/models/list-filter/filter-ast";
import { type ASTDragItem, findASTNodePreview } from "./filter-builder-types";

export const FilterDragPreview: React.FC<{
  draggedItem: ASTDragItem;
  root: FilterASTGroupNode;
}> = ({ draggedItem, root }) => {
  const intl = useIntl();
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

  const fieldLabel = intl.formatMessage({
    id: preview.criterion.criterionOption.messageID,
  });

  return (
    <div className="filter-drag-overlay">
      <div className="text-[10px] font-semibold uppercase tracking-wider opacity-60">
        Condition
      </div>
      <div className="text-sm font-medium">{fieldLabel}</div>
    </div>
  );
};
