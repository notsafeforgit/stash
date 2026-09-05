import { Label } from "@/components/ui/label";
import { useId } from "react";
import type React from "react";
import { useIntl } from "react-intl";
import {
  type DuplicatedCriterion,
  type DuplicationFieldId,
  DUPLICATION_FIELD_IDS,
  DUPLICATION_FIELD_MESSAGE_IDS,
} from "src/models/list-filter/criteria/phash";
import { IndeterminateCheckbox } from "src/components/ui/indeterminate-checkbox";

interface DuplicatedFilter {
  criterion: DuplicatedCriterion;
  setCriterion: (c: DuplicatedCriterion) => void;
}

export const DuplicatedFilter: React.FC<DuplicatedFilter> = ({
  criterion,
  setCriterion,
}) => {
  const intl = useIntl();
  const controlId = useId();

  function onFieldChange(
    fieldId: DuplicationFieldId,
    value: boolean | undefined,
  ) {
    const c = criterion.clone();
    if (value === undefined) {
      delete c.value[fieldId];
    } else {
      c.value[fieldId] = value;
    }
    setCriterion(c);
  }

  return (
    <div className="duplicated-filter">
      {DUPLICATION_FIELD_IDS.map((fieldId) => (
        <Label
          htmlFor={`${controlId}-${fieldId}`}
          key={fieldId}
          className="duplicated-filter-item"
        >
          <IndeterminateCheckbox
            id={`${controlId}-${fieldId}`}
            checked={criterion.value[fieldId]}
            setChecked={(v) => onFieldChange(fieldId, v)}
          />
          {intl.formatMessage({ id: DUPLICATION_FIELD_MESSAGE_IDS[fieldId] })}
        </Label>
      ))}
    </div>
  );
};
