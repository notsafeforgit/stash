import type React from "react";
import { useId } from "react";
import { useIntl } from "react-intl";
import type { HierarchicalCountCriterion } from "src/models/list-filter/criteria/criterion";
import type {
  IHierarchicalCountValue,
  INumberValue,
} from "src/models/list-filter/types";
import { NumberFilter } from "./number-filter";
import { DepthSelector } from "./selectable-filter";

interface HierarchicalCountFilterProps {
  criterion: HierarchicalCountCriterion;
  onValueChanged: (value: IHierarchicalCountValue) => void;
  renderNumberInput?: (
    value: number,
    onChange: (v: number) => void,
    min?: number,
  ) => React.ReactNode;
}

export const HierarchicalCountFilter: React.FC<
  HierarchicalCountFilterProps
> = ({ criterion, onValueChanged, renderNumberInput }) => {
  const intl = useIntl();
  const id = useId();

  function onNumberValueChanged(value: INumberValue) {
    onValueChanged({
      ...criterion.value,
      ...value,
    });
  }

  function onDepthChanged(depth: number) {
    onValueChanged({
      ...criterion.value,
      depth,
    });
  }

  return (
    <div className="flex flex-col gap-2">
      <NumberFilter
        criterion={criterion}
        onValueChanged={onNumberValueChanged}
        renderNumberInput={renderNumberInput}
      />
      <DepthSelector
        depth={criterion.value.depth}
        id={`${id}-include-sub-tags`}
        label={intl.formatMessage({ id: "include_sub_tags" })}
        onDepthChanged={onDepthChanged}
      />
    </div>
  );
};
