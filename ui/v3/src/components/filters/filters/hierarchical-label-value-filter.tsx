import type React from "react";
import { type MessageDescriptor, useIntl } from "react-intl";
import { Checkbox } from "src/components/ui/checkbox";
import type { ModifierCriterion } from "src/models/list-filter/criteria/criterion";
import type { IHierarchicalLabelValue } from "src/models/list-filter/types";
import { DepthSelector } from "./selectable-filter";

interface HierarchicalLabelValueFilterProps {
  criterion: ModifierCriterion<IHierarchicalLabelValue>;
  onValueChanged: (value: IHierarchicalLabelValue) => void;
  mode?: "full" | "toggle-only" | "select-only";
}

export const HierarchicalLabelValueFilter: React.FC<
  HierarchicalLabelValueFilterProps
> = ({ criterion, onValueChanged, mode = "full" }) => {
  const criterionOption = criterion.modifierCriterionOption();
  const { type, inputType } = criterionOption;

  const intl = useIntl();

  if (
    inputType !== "studios" &&
    inputType !== "tags" &&
    inputType !== "scene_tags" &&
    inputType !== "performer_tags" &&
    inputType !== "studio_tags" &&
    inputType !== "groups"
  ) {
    return null;
  }

  function onDepthChanged(depth: number) {
    const { value } = criterion;
    value.depth = depth;
    onValueChanged(value);
  }

  function criterionOptionTypeToIncludeID(): string {
    if (inputType === "studios") {
      return "include-sub-studios";
    }
    if (inputType === "groups") {
      return "include-sub-groups";
    }
    if (type === "children") {
      return "include-parent-tags";
    }
    console.log(inputType);
    return "include-sub-tags";
  }

  function criterionOptionTypeToIncludeUIString(): MessageDescriptor {
    let id: string;
    if (inputType === "studios") {
      id = "include_sub_studios";
    } else if (inputType === "groups") {
      id = "include_sub_groups";
    } else if (type === "children") {
      id = "include_parent_tags";
    } else {
      id = "include_sub_tags";
    }

    return {
      id,
    };
  }

  const includeToggle =
    inputType === "groups" ||
    inputType === "tags" ||
    inputType === "scene_tags" ||
    inputType === "performer_tags" ||
    inputType === "studio_tags" ? (
      <DepthSelector
        depth={criterion.value.depth}
        id={criterionOptionTypeToIncludeID()}
        label={intl.formatMessage(criterionOptionTypeToIncludeUIString())}
        onDepthChanged={onDepthChanged}
      />
    ) : (
      <div className="flex items-center gap-2">
        <Checkbox
          id={criterionOptionTypeToIncludeID()}
          checked={criterion.value.depth !== 0}
          onCheckedChange={() =>
            onDepthChanged(criterion.value.depth !== 0 ? 0 : -1)
          }
        />
        <label htmlFor={criterionOptionTypeToIncludeID()}>
          {intl.formatMessage(criterionOptionTypeToIncludeUIString())}
        </label>
      </div>
    );

  if (mode === "toggle-only") {
    return <>{includeToggle}</>;
  }

  // select-only and full modes: no standalone select control here
  // (entity-specific filters handle selection via dedicated filter components)
  return null;
};
