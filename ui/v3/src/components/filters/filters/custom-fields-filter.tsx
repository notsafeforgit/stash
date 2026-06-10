import React, { useEffect, useMemo, useState } from "react";
import type { CustomFieldsCriterion } from "src/models/list-filter/criteria/custom-fields";
import {
  CriterionModifier,
  type CustomFieldCriterionInput,
} from "src/core/generated-graphql";
import { ModifierSelect } from "../modifier-select";
import { Input } from "src/components/ui/input";
import { Button } from "src/components/ui/button";
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
  if (!Number.isNaN(num)) {
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
    <div className="flex flex-col gap-2">
      <div className="flex flex-col gap-2">
        <div className="flex gap-2">
          <Input
            className="flex-1"
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
        {modifier !== CriterionModifier.IsNull &&
          modifier !== CriterionModifier.NotNull && (
            <div className="flex gap-2">
              <Input
                placeholder={firstPlaceholder}
                className={hasTwoValues ? "flex-1" : "w-full"}
                type="text"
                onChange={(e) => setFirstValue(e.target.value)}
                value={firstValue}
              />
              {hasTwoValues && (
                <Input
                  placeholder={intl.formatMessage({
                    id: "criterion.less_than",
                  })}
                  className="flex-1"
                  type="text"
                  onChange={(e) => setSecondValue(e.target.value)}
                  value={secondValue}
                />
              )}
            </div>
          )}
      </div>
      <div className="flex items-center gap-1 self-end">
        <Button
          type="button"
          variant="default"
          size="icon-sm"
          onClick={() => onConfirm()}
          disabled={!field}
        >
          <Check size={16} />
        </Button>
        {editing && (
          <Button
            type="button"
            variant="secondary"
            size="icon-sm"
            onClick={() => cancel()}
          >
            <X size={16} />
          </Button>
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
    <span
      className="inline-flex items-center gap-1 px-2 py-0.5 text-xs rounded-md bg-secondary text-secondary-foreground cursor-pointer hover:bg-secondary/80"
      onClick={onEditCriterion}
    >
      {label}
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        onClick={(e) => {
          onRemoveCriterion();
          e.stopPropagation();
        }}
      >
        <X size={14} />
      </Button>
    </span>
  );
};

const CustomFieldsCriteriaPills: React.FC<{
  criteria: CustomFieldCriterionInput[];
  editIndex?: number;
  onEditCriterion: (index: number) => void;
  onRemoveCriterion: (index: number) => void;
}> = ({ criteria, editIndex, onEditCriterion, onRemoveCriterion }) => {
  if (criteria.length === 0) return null;

  return (
    <div className="flex flex-wrap gap-1 justify-center mb-2">
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
    <div className="flex flex-col gap-2">
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
