import type React from "react";
import { useMemo } from "react";
import { useIntl } from "react-intl";
import { useQuery } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { CriterionModifier, type FilterMode } from "src/core/generated-graphql";
import type { SingleCustomFieldCriterion } from "src/models/list-filter/criteria/custom-fields";
import { Input } from "src/components/ui/input";
import { ModifierSelect } from "../modifier-select";
import {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  ComboboxCollection,
  ComboboxEmpty,
} from "src/components/ui/combobox";
import { Spinner } from "src/components/ui/spinner";

const MODIFIERS = [
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

function coerceValue(v: string): string | number {
  if (v === "") return v;
  const num = Number(v);
  return Number.isFinite(num) && v.trim() !== "" ? num : v;
}

interface SingleCustomFieldFilterProps {
  criterion: SingleCustomFieldCriterion;
  setCriterion: (c: SingleCustomFieldCriterion) => void;
  mode: FilterMode;
}

export const SingleCustomFieldFilter: React.FC<
  SingleCustomFieldFilterProps
> = ({ criterion, setCriterion, mode }) => {
  const intl = useIntl();

  const { data, loading } = useQuery(GQL.CustomFieldNamesDocument, {
    variables: { mode },
    fetchPolicy: "cache-and-network",
  });

  const fieldOptions = useMemo<string[]>(
    () => data?.customFieldNames ?? [],
    [data],
  );

  // Allow the current field name to appear in the dropdown even if it isn't
  // (yet) returned by the server — protects against decoded saved filters
  // whose field has since been renamed/removed in the data.
  const optionsWithCurrent = useMemo(() => {
    if (!criterion.field || fieldOptions.includes(criterion.field)) {
      return fieldOptions;
    }
    return [criterion.field, ...fieldOptions];
  }, [fieldOptions, criterion.field]);

  function update(patch: Partial<SingleCustomFieldCriterion>) {
    const next = criterion.clone() as SingleCustomFieldCriterion;
    Object.assign(next, patch);
    setCriterion(next);
  }

  function onChangeField(name: string | null) {
    update({ field: name ?? "" });
  }

  function onChangeModifier(m: CriterionModifier) {
    if (m === CriterionModifier.IsNull || m === CriterionModifier.NotNull) {
      update({ modifier: m, value: [] });
    } else {
      update({ modifier: m });
    }
  }

  function onFirstValue(v: string) {
    const nv = coerceValue(v);
    if (
      criterion.modifier === CriterionModifier.Between ||
      criterion.modifier === CriterionModifier.NotBetween
    ) {
      const second = criterion.value[1] ?? "";
      update({ value: [nv, second] });
    } else {
      update({ value: [nv] });
    }
  }

  function onSecondValue(v: string) {
    const first = criterion.value[0] ?? "";
    update({ value: [first, coerceValue(v)] });
  }

  const isNullCheck =
    criterion.modifier === CriterionModifier.IsNull ||
    criterion.modifier === CriterionModifier.NotNull;
  const isBetween =
    criterion.modifier === CriterionModifier.Between ||
    criterion.modifier === CriterionModifier.NotBetween;

  const firstValue = (criterion.value[0] as string | number | undefined) ?? "";
  const secondValue = (criterion.value[1] as string | number | undefined) ?? "";

  const fieldPlaceholder = loading
    ? intl.formatMessage({ id: "loading.generic", defaultMessage: "Loading…" })
    : fieldOptions.length === 0
      ? intl.formatMessage({
          id: "custom_fields.no_fields_available",
          defaultMessage: "No custom fields exist yet",
        })
      : intl.formatMessage({ id: "custom_fields.field" });

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-col sm:flex-row gap-2">
        <Combobox
          value={criterion.field || null}
          onValueChange={onChangeField}
          items={optionsWithCurrent}
        >
          <ComboboxInput
            placeholder={fieldPlaceholder}
            disabled={loading && fieldOptions.length === 0}
            className="flex-1"
            showClear={!!criterion.field}
          />
          <ComboboxContent>
            {loading && fieldOptions.length === 0 ? (
              <div className="flex items-center justify-center py-3">
                <Spinner />
              </div>
            ) : (
              <>
                <ComboboxEmpty>
                  {intl.formatMessage({
                    id: "filter_no_results",
                    defaultMessage: "No matches",
                  })}
                </ComboboxEmpty>
                <ComboboxList>
                  <ComboboxCollection>
                    {(name: string) => (
                      <ComboboxItem key={name} value={name}>
                        {name}
                      </ComboboxItem>
                    )}
                  </ComboboxCollection>
                </ComboboxList>
              </>
            )}
          </ComboboxContent>
        </Combobox>
        <div className="sm:w-1/3">
          <ModifierSelect
            options={MODIFIERS}
            value={criterion.modifier}
            onChanged={onChangeModifier}
          />
        </div>
      </div>
      {!isNullCheck && (
        <div className="flex gap-2">
          <Input
            type="text"
            placeholder={
              isBetween
                ? intl.formatMessage({ id: "criterion.greater_than" })
                : intl.formatMessage({ id: "custom_fields.value" })
            }
            className={isBetween ? "flex-1" : "w-full"}
            value={String(firstValue)}
            onChange={(e) => onFirstValue(e.target.value)}
            disabled={!criterion.field}
          />
          {isBetween && (
            <Input
              type="text"
              placeholder={intl.formatMessage({ id: "criterion.less_than" })}
              className="flex-1"
              value={String(secondValue)}
              onChange={(e) => onSecondValue(e.target.value)}
              disabled={!criterion.field}
            />
          )}
        </div>
      )}
    </div>
  );
};
