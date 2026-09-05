import type React from "react";
import { Button } from "src/components/ui/button";
import { useIntl } from "react-intl";
import type { ListFilterModel } from "src/models/list-filter/filter";
import {
  flattenFilterASTConditions,
  filterASTConditionLabel,
} from "src/models/list-filter/filter-ast";
const MAX_VISIBLE_SUMMARY_CONDITIONS = 4;

const TagItem: React.FC<{
  className?: string;
  onClick?: () => void;
  title?: string;
  children: React.ReactNode;
}> = ({ className, onClick, title, children }) => (
  <Button
    variant="secondary"
    size="sm"
    className={`inline-flex cursor-pointer items-center rounded-full bg-muted px-2 py-0.5 text-xs hover:bg-muted/70 ${className ?? ""}`}
    onClick={onClick}
    title={title}
  >
    {children}
  </Button>
);

export const FilterASTSummary: React.FC<{
  filter: ListFilterModel;
  onEdit: () => void;
  onClear: () => void;
}> = ({ filter, onEdit, onClear }) => {
  const intl = useIntl();

  if (!filter.filterAst) {
    return null;
  }

  const conditions = flattenFilterASTConditions(filter.filterAst);
  const rootLabel =
    filter.filterAst.kind === "group" && filter.filterAst.operator === "OR"
      ? "Any condition"
      : "All conditions";
  const conditionLabels = conditions.map((condition) =>
    filterASTConditionLabel(condition, intl, false),
  );
  const visibleLabels = conditionLabels.slice(
    0,
    MAX_VISIBLE_SUMMARY_CONDITIONS,
  );
  const hiddenCount = Math.max(
    0,
    conditionLabels.length - MAX_VISIBLE_SUMMARY_CONDITIONS,
  );

  return (
    <div className="filter-ast-summary">
      <div className="filter-ast-summary-tags">
        <TagItem className="filter-ast-root-tag" onClick={() => onEdit()}>
          {rootLabel}
        </TagItem>
        {visibleLabels.map((label, index) => (
          <TagItem
            key={`${label}-${index}`}
            className="filter-ast-condition-tag"
            onClick={() => onEdit()}
            title={label}
          >
            {label}
          </TagItem>
        ))}
        {hiddenCount > 0 && (
          <TagItem className="filter-ast-more-tag" onClick={() => onEdit()}>
            +{hiddenCount} more
          </TagItem>
        )}
      </div>
      <Button
        variant="ghost"
        size="sm"
        className="clear-all-button filter-ast-clear"
        onClick={onClear}
      >
        Clear all
      </Button>
    </div>
  );
};
