import type React from "react";
import { useIntl } from "react-intl";
import { Input } from "src/components/ui/input";
import type { IStashIDValue } from "../../../models/list-filter/types";
import type { ModifierCriterion } from "../../../models/list-filter/criteria/criterion";
import { CriterionModifier } from "src/core/generated-graphql";

interface StashIDFilterProps {
  criterion: ModifierCriterion<IStashIDValue>;
  onValueChanged: (value: IStashIDValue) => void;
}

export const StashIDFilter: React.FC<StashIDFilterProps> = ({
  criterion,
  onValueChanged,
}) => {
  const intl = useIntl();
  const { value } = criterion;

  return (
    <div>
      <Input
        type="text"
        onChange={(e) =>
          onValueChanged({
            endpoint: e.target.value,
            stashID: criterion.value.stashID,
          })
        }
        value={value ? value.endpoint : ""}
        placeholder={intl.formatMessage({ id: "stash_id_endpoint" })}
      />
      {criterion.modifier !== CriterionModifier.IsNull &&
        criterion.modifier !== CriterionModifier.NotNull && (
          <Input
            type="text"
            onChange={(e) =>
              onValueChanged({
                stashID: e.target.value,
                endpoint: criterion.value.endpoint,
              })
            }
            value={value ? value.stashID : ""}
            placeholder={intl.formatMessage({ id: "stash_id" })}
          />
        )}
    </div>
  );
};
