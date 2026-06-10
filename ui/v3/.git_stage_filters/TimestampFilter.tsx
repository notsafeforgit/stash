import React from "react";
import { useIntl } from "react-intl";
import { CriterionModifier } from "../../../core/generated-graphql";
import { ITimestampValue } from "../../../models/list-filter/types";
import { ModifierCriterion } from "../../../models/list-filter/criteria/criterion";
import { DateInput } from "src/components/ui/date-input";

interface TimestampFilterProps {
  criterion: ModifierCriterion<ITimestampValue>;
  onValueChanged: (value: ITimestampValue) => void;
}

function parseDateOnly(ts: string): Date | undefined {
  const datePart = ts.trim().slice(0, 10);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(datePart)) return undefined;
  const d = new Date(datePart + "T00:00:00");
  return isNaN(d.getTime()) ? undefined : d;
}

function adjustTimestamp(ts: string, minutesDelta: number): string {
  if (!/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/.test(ts)) return ts;
  const d = new Date(ts.replace(" ", "T") + ":00");
  if (isNaN(d.getTime())) return ts;
  d.setMinutes(d.getMinutes() + minutesDelta);
  return (
    [
      d.getFullYear(),
      String(d.getMonth() + 1).padStart(2, "0"),
      String(d.getDate()).padStart(2, "0"),
    ].join("-") +
    " " +
    [
      String(d.getHours()).padStart(2, "0"),
      String(d.getMinutes()).padStart(2, "0"),
    ].join(":")
  );
}

function adjustDate(dateStr: string, daysDelta: number): string {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(dateStr)) return dateStr;
  const d = new Date(dateStr + "T00:00:00");
  if (isNaN(d.getTime())) return dateStr;
  d.setDate(d.getDate() + daysDelta);
  return [
    d.getFullYear(),
    String(d.getMonth() + 1).padStart(2, "0"),
    String(d.getDate()).padStart(2, "0"),
  ].join("-");
}

// Advance by the minimum unit: full timestamp → +1 min, date-only → +1 day.
function bumpUp(ts: string): string {
  if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/.test(ts)) return adjustTimestamp(ts, 1);
  if (/^\d{4}-\d{2}-\d{2}$/.test(ts)) return adjustDate(ts, 1);
  return ts;
}

// Retreat by the minimum unit.
function bumpDown(ts: string): string {
  if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/.test(ts))
    return adjustTimestamp(ts, -1);
  if (/^\d{4}-\d{2}-\d{2}$/.test(ts)) return adjustDate(ts, -1);
  return ts;
}

export const TimestampFilter: React.FC<TimestampFilterProps> = ({
  criterion,
  onValueChanged,
}) => {
  const intl = useIntl();

  const { value } = criterion;

  const isBetween =
    criterion.modifier === CriterionModifier.Between ||
    criterion.modifier === CriterionModifier.NotBetween;

  let equalsControl: React.JSX.Element | null = null;
  if (
    criterion.modifier === CriterionModifier.Equals ||
    criterion.modifier === CriterionModifier.NotEquals
  ) {
    equalsControl = (
      <DateInput
        value={value?.value ?? ""}
        onValueChange={(v) => onValueChanged({ ...value, value: v })}
        placeholder={intl.formatMessage({ id: "criterion.value" })}
        isTime
      />
    );
  }

  let lowerControl: React.JSX.Element | null = null;
  if (criterion.modifier === CriterionModifier.GreaterThan || isBetween) {
    const handleLowerChange = isBetween
      ? (v: string) => {
          const upper = value?.value2 ?? "";
          onValueChanged({
            ...value,
            value: v,
            value2: v && upper && v > upper ? bumpUp(v) : upper,
          });
        }
      : (v: string) => onValueChanged({ ...value, value: v });

    const upperDate = isBetween
      ? parseDateOnly(value?.value2 ?? "")
      : undefined;

    lowerControl = (
      <DateInput
        value={value?.value ?? ""}
        onValueChange={handleLowerChange}
        placeholder={intl.formatMessage({ id: "criterion.greater_than" })}
        isTime
        disabledDays={upperDate ? (d) => d > upperDate : undefined}
      />
    );
  }

  let upperControl: React.JSX.Element | null = null;
  if (criterion.modifier === CriterionModifier.LessThan || isBetween) {
    const property = isBetween ? "value2" : "value";
    const handleUpperChange = isBetween
      ? (v: string) => {
          const lower = value?.value ?? "";
          onValueChanged({
            ...value,
            value: v && lower && v < lower ? bumpDown(v) : lower,
            value2: v,
          });
        }
      : (v: string) => onValueChanged({ ...value, [property]: v });

    const lowerDate = isBetween ? parseDateOnly(value?.value ?? "") : undefined;

    upperControl = (
      <DateInput
        value={
          (criterion.modifier === CriterionModifier.LessThan
            ? value?.value
            : value?.value2) ?? ""
        }
        onValueChange={handleUpperChange}
        placeholder={intl.formatMessage({ id: "criterion.less_than" })}
        isTime
        disabledDays={lowerDate ? (d) => d < lowerDate : undefined}
      />
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
