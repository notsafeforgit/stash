import React, { useEffect, useMemo, useState } from "react";
import { CustomFieldsCriterion } from "src/models/list-filter/criteria/custom-fields";
import {
  CriterionModifier,
  CustomFieldCriterionInput,
} from "src/core/generated-graphql";
import { ModifierSelect } from "../ModifierSelect";
import { Input } from "src/components/ui/input";
import { useIntl } from "react-intl";
import { Check, Pencil, X } from "lucide-react";
import { ModifierCriterion } from "src/models/list-filter/criteria/criterion";

const CUSTOM_FIELD_MODIFIERS = [
  CriterionModifier.IsNull,
  CriterionModifier.NotNull,
  CriterionModifier.Equals,
  CriterionModifier.NotEquals,
  CriterionModifier.Includes,
  CriterionModifier.Excludes,
  CriterionModifier.GreaterThan,
  CriterionModifier.LessThan,
  CriterionModifier.Between,
  CriterionModifier.NotBetween,
];

interface CustomFieldCriterionEditorProps {
  criterion?: CustomFieldCriterionInput;
  setCriterion: (c: CustomFieldCriterionInput) => void;
  cancel: () => void;
  editing?: boolean;
}

function getValue(v: string) {
  const num = Number(v);
  if (!isNaN(num)) {
    return num;
  } else {
    return v;
  }
}

const CustomFieldCriterionEditor: React.FC<CustomFieldCriterionEditorProps> = ({
  criterion,
  setCriterion,
  editing = false,
  cancel,
}) => {
  const intl = useIntl();

  const [field, setField] = React.useState(criterion?.field ?? "");
  const [value, setValue] = React.useState(criterion?.value);
  const [modifier, setModifier] = React.useState(
    criterion?.modifier ?? CriterionModifier.Equals,
  );

  const firstValue = value && value.length > 0 ? (value[0] as string) : "";
  const secondValue = value && value.length > 1 ? (value[1] as string) : "";

  useEffect(() => {
    setField((criterion?.field as string) ?? "");
    setValue(criterion?.value ?? []);
    setModifier(criterion?.modifier ?? CriterionModifier.Equals);
  }, [criterion]);

  function setFirstValue(v: string) {
    const nv = getValue(v);
    if (
      modifier === CriterionModifier.Between ||
      modifier === CriterionModifier.NotBetween
    ) {
      setValue([nv, secondValue]);
    } else {
      setValue([nv]);
    }
  }

  function setSecondValue(v: string) {
    setValue([firstValue, getValue(v)]);
  }

  function onChangeModifier(m: CriterionModifier) {
    setModifier(m);
    if (m === CriterionModifier.IsNull || m === CriterionModifier.NotNull) {
      setValue(undefined);
    }
  }

  function onConfirm() {
    setCriterion({
      field,
      value,
      modifier,
    });
  }

  const firstPlaceholder =
    modifier === CriterionModifier.Between ||
    modifier === CriterionModifier.NotBetween
      ? intl.formatMessage({ id: "criterion.greater_than" })
      : intl.formatMessage({ id: "custom_fields.value" });

  const hasTwoValues =
    modifier === CriterionModifier.Between ||
    modifier === CriterionModifier.NotBetween;

  return (
    <div className="custom-field-filter">
      <div>
        <div className="custom-field-filter-row">
          <Input
            className="btn-secondary"
            type="text"
            placeholder={intl.formatMessage({ id: "custom_fields.field" })}
            onChange={(e) => setField(e.target.value)}
            value={field}
          />
          <ModifierSelect
            options={CUSTOM_FIELD_MODIFIERS}
            value={modifier}
            onChanged={(m) => onChangeModifier(m)}
          />
        </div>
        <div className="custom-field-filter-row">
          {modifier !== CriterionModifier.IsNull &&
            modifier !== CriterionModifier.NotNull && (
              <Input
                placeholder={firstPlaceholder}
                className={`btn-secondary${hasTwoValues ? " half-width" : ""}`}
                type="text"
                onChange={(e) => setFirstValue(e.target.value)}
                value={firstValue}
              />
            )}
          {(modifier === CriterionModifier.Between ||
            modifier === CriterionModifier.NotBetween) && (
            <Input
              placeholder={intl.formatMessage({ id: "criterion.less_than" })}
              className="btn-secondary half-width"
              type="text"
              onChange={(e) => setSecondValue(e.target.value)}
              value={secondValue}
            />
          )}
        </div>
      </div>
      <div className="custom-field-filter-buttons">
        <button
          className="btn btn-success"
          onClick={() => onConfirm()}
          disabled={!field}
        >
          <Check className="icon" size={16} />
        </button>
        {editing && (
          <button className="btn btn-secondary" onClick={() => cancel()}>
            <X className="icon" size={16} />
          </button>
        )}
      </div>
    </div>
  );
};

function valueToString(value: unknown[] | undefined | null) {
  if (!value) return "";
  return value.map((v) => v as string).join(", ");
}

const CustomFieldFilterTag: React.FC<{
  criterion: CustomFieldCriterionInput;
  editing?: boolean;
  onEditCriterion: () => void;
  onRemoveCriterion: () => void;
}> = ({ criterion, editing, onEditCriterion, onRemoveCriterion }) => {
  const intl = useIntl();

  const label = useMemo(() => {
    const { field, modifier, value } = criterion;
    const modifierString = ModifierCriterion.getModifierLabel(intl, modifier);

    const str = intl.formatMessage(
      { id: "criterion_modifier.format_string" },
      {
        criterion: field,
        modifierString,
        valueString: valueToString(value),
      },
    );

    if (editing) {
      return (
        <span>
          <Pencil className="icon" size={16} />
          {str}
        </span>
      );
    }

    return <>{str}</>;
  }, [criterion, editing, intl]);

  return (
    <span className="tag-item" onClick={onEditCriterion}>
      {label}
      <button
        className="btn btn-secondary"
        onClick={(e) => {
          onRemoveCriterion();
          e.stopPropagation();
        }}
      >
        <X size={14} />
      </button>
    </span>
  );
};

const CustomFieldsCriteriaPills: React.FC<{
  criteria: CustomFieldCriterionInput[];
  editIndex?: number;
  onEditCriterion: (index: number) => void;
  onRemoveCriterion: (index: number) => void;
}> = ({ criteria, editIndex, onEditCriterion, onRemoveCriterion }) => {
  return (
    <div className="d-flex justify-content-center mb-2 wrap-tags filter-tags">
      {criteria.map((c, index) => (
        <CustomFieldFilterTag
          key={index}
          editing={index === editIndex}
          criterion={c}
          onEditCriterion={() => onEditCriterion(index)}
          onRemoveCriterion={() => onRemoveCriterion(index)}
        />
      ))}
    </div>
  );
};

interface CustomFieldsFilterProps {
  criterion: CustomFieldsCriterion;
  setCriterion: (c: CustomFieldsCriterion) => void;
}

function initCriterion(
  criterion: CustomFieldsCriterion,
): CustomFieldsCriterion {
  return criterion.clone() as CustomFieldsCriterion;
}

function createNewCriterion(): CustomFieldCriterionInput {
  return {
    field: "",
    value: [],
    modifier: CriterionModifier.Equals,
  };
}

export const CustomFieldsFilter: React.FC<CustomFieldsFilterProps> = ({
  criterion,
  setCriterion,
}) => {
  const [localCriterion, setLocalCriterion] = React.useState(
    initCriterion(criterion),
  );

  const [editCriterion, setEditCriterion] = useState(createNewCriterion());
  const editIndex = useMemo(
    () => localCriterion.value.indexOf(editCriterion),
    [localCriterion, editCriterion],
  );

  function updateCriteria(newCriteria: CustomFieldCriterionInput[]) {
    const validCriteria = newCriteria.filter((c) => c.field !== "");
    const newValue = criterion.clone() as CustomFieldsCriterion;
    newValue.value = validCriteria;
    setCriterion(newValue);
  }

  function onChange(nv: CustomFieldCriterionInput) {
    const newValue = localCriterion.clone() as CustomFieldsCriterion;

    if (editIndex === -1) {
      newValue.value.push(nv);
    } else {
      newValue.value[editIndex] = nv;
    }

    setLocalCriterion(newValue);
    updateCriteria(newValue.value);
    setEditCriterion(createNewCriterion());
  }

  function onRemove(index: number) {
    const c = localCriterion.clone() as CustomFieldsCriterion;
    c.value.splice(index, 1);
    setLocalCriterion(c);
    updateCriteria(c.value);
    if (index === editIndex) {
      setEditCriterion(createNewCriterion());
    }
  }

  return (
    <div>
      <CustomFieldCriterionEditor
        criterion={editCriterion}
        editing={editCriterion.field !== ""}
        setCriterion={onChange}
        cancel={() => setEditCriterion(createNewCriterion())}
      />
      <CustomFieldsCriteriaPills
        criteria={localCriterion.value}
        editIndex={editIndex !== -1 ? editIndex : undefined}
        onEditCriterion={(index) =>
          setEditCriterion(localCriterion.value[index])
        }
        onRemoveCriterion={(index) => onRemove(index)}
      />
    </div>
  );
};
