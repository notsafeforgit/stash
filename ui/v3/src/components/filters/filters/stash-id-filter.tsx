import type React from "react";
import { useIntl } from "react-intl";
import { Input } from "src/components/ui/input";
import { Field, FieldGroup } from "src/components/ui/field";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import type { IStashIDValue } from "../../../models/list-filter/types";
import type { ModifierCriterion } from "../../../models/list-filter/criteria/criterion";
import { CriterionModifier } from "src/core/generated-graphql";
import { useConfigurationContext } from "src/hooks/config";
import { stashboxDisplayName } from "src/utils/stashbox";

const ANY_ENDPOINT_VALUE = "__any_stash_box_endpoint__";

interface StashIDFilterProps {
  criterion: ModifierCriterion<IStashIDValue>;
  onValueChanged: (value: IStashIDValue) => void;
}

export const StashIDFilter: React.FC<StashIDFilterProps> = ({
  criterion,
  onValueChanged,
}) => {
  const intl = useIntl();
  const { configuration } = useConfigurationContext();
  const { value } = criterion;
  const stashBoxes = configuration.general.stashBoxes ?? [];
  const selectedEndpoint = value?.endpoint ?? "";
  const selectedBoxIndex = stashBoxes.findIndex(
    (box) => box.endpoint === selectedEndpoint,
  );
  const selectedLabel =
    selectedBoxIndex >= 0
      ? stashboxDisplayName(stashBoxes[selectedBoxIndex].name, selectedBoxIndex)
      : selectedEndpoint;
  const anyEndpointLabel = intl.formatMessage({
    id: "stash_id_endpoint_any",
    defaultMessage: "Any endpoint",
  });

  return (
    <FieldGroup className="gap-2">
      <Field>
        <Select
          value={selectedEndpoint || ANY_ENDPOINT_VALUE}
          onValueChange={(next) =>
            onValueChanged({
              endpoint:
                !next || next === ANY_ENDPOINT_VALUE ? "" : String(next),
              stashID: criterion.value.stashID,
            })
          }
        >
          <SelectTrigger
            className="w-full"
            aria-label={intl.formatMessage({ id: "stash_id_endpoint" })}
          >
            <SelectValue>
              {selectedEndpoint ? selectedLabel : anyEndpointLabel}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value={ANY_ENDPOINT_VALUE}>
                {anyEndpointLabel}
              </SelectItem>
              {stashBoxes.map((stashBox, index) => (
                <SelectItem key={stashBox.endpoint} value={stashBox.endpoint}>
                  {stashboxDisplayName(stashBox.name, index)}
                </SelectItem>
              ))}
              {selectedEndpoint && selectedBoxIndex < 0 && (
                <SelectItem value={selectedEndpoint}>
                  {selectedEndpoint}
                </SelectItem>
              )}
            </SelectGroup>
          </SelectContent>
        </Select>
      </Field>
      {criterion.modifier !== CriterionModifier.IsNull &&
        criterion.modifier !== CriterionModifier.NotNull && (
          <Field>
            <Input
              type="text"
              aria-label={intl.formatMessage({ id: "stash_id" })}
              onChange={(e) =>
                onValueChanged({
                  stashID: e.target.value,
                  endpoint: criterion.value.endpoint,
                })
              }
              value={value ? value.stashID : ""}
              placeholder={intl.formatMessage({ id: "stash_id" })}
            />
          </Field>
        )}
    </FieldGroup>
  );
};
