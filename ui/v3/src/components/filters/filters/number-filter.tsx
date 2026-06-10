import type React from "react";
import { useIntl } from "react-intl";
import { Minus, Plus } from "lucide-react";
import { Input } from "src/components/ui/input";
import { Button } from "src/components/ui/button";
import { CriterionModifier } from "../../../core/generated-graphql";
import type { INumberValue } from "../../../models/list-filter/types";
import type { NumberCriterion } from "../../../models/list-filter/criteria/criterion";

interface NumberFilterProps {
  criterion: NumberCriterion;
  onValueChanged: (value: INumberValue) => void;
  renderNumberInput?: (
    value: number,
    onChange: (v: number) => void,
    min?: number,
  ) => React.ReactNode;
}

export const NumberFilter: React.FC<NumberFilterProps> = ({
  criterion,
  onValueChanged,
  renderNumberInput,
}) => {
  const intl = useIntl();

  const { value } = criterion;

  function renderField(
    property: "value" | "value2",
    placeholder: string,
    onChange?: (v: number) => void,
    min?: number,
  ): React.JSX.Element {
    const numericValue = value?.[property] ?? 0;
    const handleChange =
      onChange ?? ((v: number) => onValueChanged({ ...value, [property]: v }));

    if (renderNumberInput) {
      return <>{renderNumberInput(numericValue, handleChange, min)}</>;
    }

    const minValue = min ?? 0;

    return (
      <div className="flex items-center gap-1">
        <Button
          variant="outline"
          size="icon"
          className="shrink-0"
          onClick={() => handleChange(Math.max(minValue, numericValue - 1))}
          onPointerDown={(e) => e.preventDefault()}
          tabIndex={-1}
        >
          <Minus className="size-4" />
        </Button>
        <Input
          type="text"
          inputMode="numeric"
          value={numericValue === 0 ? "" : String(numericValue)}
          onChange={(e) => {
            const raw = e.target.value.replace(/[^0-9]/g, "");
            handleChange(raw === "" ? 0 : parseInt(raw, 10));
          }}
          placeholder={placeholder}
          className="text-center"
        />
        <Button
          variant="outline"
          size="icon"
          className="shrink-0"
          onClick={() => handleChange(numericValue + 1)}
          onPointerDown={(e) => e.preventDefault()}
          tabIndex={-1}
        >
          <Plus className="size-4" />
        </Button>
      </div>
    );
  }

  const isBetween =
    criterion.modifier === CriterionModifier.Between ||
    criterion.modifier === CriterionModifier.NotBetween;

  let equalsControl: React.JSX.Element | null = null;
  if (
    criterion.modifier === CriterionModifier.Equals ||
    criterion.modifier === CriterionModifier.NotEquals
  ) {
    equalsControl = renderField(
      "value",
      intl.formatMessage({ id: "criterion.value" }),
    );
  }

  let lowerControl: React.JSX.Element | null = null;
  if (criterion.modifier === CriterionModifier.GreaterThan || isBetween) {
    const onLowerChange = isBetween
      ? (v: number) => {
          const upper = value?.value2 ?? 0;
          onValueChanged({
            ...value,
            value: v,
            value2: v >= upper ? v + 1 : upper,
          });
        }
      : undefined;
    lowerControl = renderField(
      "value",
      intl.formatMessage({ id: "criterion.greater_than" }),
      onLowerChange,
    );
  }

  let upperControl: React.JSX.Element | null = null;
  if (criterion.modifier === CriterionModifier.LessThan || isBetween) {
    const property = isBetween ? "value2" : "value";
    const onUpperChange = isBetween
      ? (v: number) => {
          const clamped = Math.max(1, v);
          const lower = value?.value ?? 0;
          onValueChanged({
            ...value,
            value: clamped <= lower ? clamped - 1 : lower,
            value2: clamped,
          });
        }
      : undefined;
    upperControl = renderField(
      property,
      intl.formatMessage({ id: "criterion.less_than" }),
      onUpperChange,
      isBetween ? 1 : undefined,
    );
  }

  return (
    <>
      {equalsControl}
      {lowerControl}
      {upperControl}
    </>
  );
};
