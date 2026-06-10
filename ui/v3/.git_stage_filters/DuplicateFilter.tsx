import React from "react";
import { useIntl } from "react-intl";
import {
  DuplicatedCriterion,
  DuplicationFieldId,
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
        <label key={fieldId} className="duplicated-filter-item">
          <IndeterminateCheckbox
            id={`duplicated-${fieldId}`}
            checked={criterion.value[fieldId]}
            setChecked={(v) => onFieldChange(fieldId, v)}
          />
          {intl.formatMessage({ id: DUPLICATION_FIELD_MESSAGE_IDS[fieldId] })}
        </label>
      ))}
    </div>
  );
};
