import React from "react";
import { useIntl } from "react-intl";
import { Input } from "src/components/ui/input";
import { IPhashDistanceValue } from "../../../models/list-filter/types";
import { ModifierCriterion } from "../../../models/list-filter/criteria/criterion";
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

  function distanceChanged(event: React.ChangeEvent<HTMLInputElement>) {
    let distance = parseInt(event.target.value);
    if (distance < 0 || isNaN(distance)) {
      distance = 0;
    }
    onValueChanged({
      distance,
      value: criterion.value.value,
    });
  }

  return (
    <div>
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
          <Input
            type="text"
            inputMode="numeric"
            onChange={distanceChanged}
            value={value ? value.distance : ""}
            placeholder={intl.formatMessage({ id: "distance" })}
          />
        )}
    </div>
  );
};
