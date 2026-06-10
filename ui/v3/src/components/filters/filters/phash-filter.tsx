import type React from "react";
import { useIntl } from "react-intl";
import { Input } from "src/components/ui/input";
import { Label } from "src/components/ui/label";
import { NumberInput } from "src/components/filters/number-input";
import type { IPhashDistanceValue } from "../../../models/list-filter/types";
import type { ModifierCriterion } from "../../../models/list-filter/criteria/criterion";
import { CriterionModifier } from "src/core/generated-graphql";

interface PhashFilterProps {
  criterion: ModifierCriterion<IPhashDistanceValue>;
  onValueChanged: (value: IPhashDistanceValue) => void;
}

export const PhashFilter: React.FC<PhashFilterProps> = ({
  criterion,
  onValueChanged,
}) => {
  const intl = useIntl();
  const { value } = criterion;

  return (
    <div className="flex flex-col gap-2">
      <Input
        type="text"
        onChange={(e) =>
          onValueChanged({
            value: e.target.value,
            distance: criterion.value.distance,
          })
        }
        value={value ? value.value : ""}
        placeholder={intl.formatMessage({ id: "media_info.phash" })}
      />
      {criterion.modifier !== CriterionModifier.IsNull &&
        criterion.modifier !== CriterionModifier.NotNull && (
          <div className="flex flex-col gap-1">
            <Label>{intl.formatMessage({ id: "distance" })}</Label>
            <NumberInput
              value={value?.distance ?? 0}
              onChange={(distance) =>
                onValueChanged({ distance, value: criterion.value.value })
              }
            />
          </div>
        )}
    </div>
  );
};
