import type { IntlShape } from "react-intl";
import { Criterion, CriterionOption, ModifierCriterion } from "./criterion";
import {
  CriterionModifier,
  type CustomFieldCriterionInput,
} from "src/core/generated-graphql";
function cloneDeep<T>(value: T): T {
  return structuredClone(value);
}

function valueToString(value: unknown[] | undefined | null) {
  if (!value) return "";
  return value.map((v) => v as string).join(", ");
}

// A compound custom-field AST condition is retained for saved selections with
// multiple fields. New builder selections use one custom_field per condition.
export const CustomFieldsCriterionOption = new CriterionOption({
  type: "custom_fields",
  messageID: "custom_fields.title",
  makeCriterion: () => new CustomFieldsCriterion(),
  hidden: true,
});

// One field per AST condition; the server evaluates the custom_fields value
// directly, while the enclosing AST supplies AND/OR composition.
export const SingleCustomFieldCriterionOption = new CriterionOption({
  type: "custom_field",
  messageID: "custom_fields.title",
  makeCriterion: () => new SingleCustomFieldCriterion(),
  serverField: "custom_fields",
});

export class CustomFieldsCriterion extends Criterion {
  public value: CustomFieldCriterionInput[] = [];

  constructor() {
    super(CustomFieldsCriterionOption);
  }

  protected cloneValues() {
    this.value = structuredClone(this.value);
  }

  public isValid(): boolean {
    return this.value.length > 0;
  }

  // AST → GraphQL serialization. The server's FilterAST condition shape is
  // `{field, value: Any}`; for a custom_fields condition the value is the
  // full array of CustomFieldCriterionInput evaluated by this one condition.
  public toCriterionInputValue(): unknown {
    return cloneDeep(this.value);
  }

  public getLabel(intl: IntlShape): string {
    // show first criterion
    if (this.value.length === 0) {
      return "";
    }

    const first = this.value[0];
    if (!first) return "";
    let messageID: string;
    let valueString = "";

    if (
      first.modifier !== CriterionModifier.IsNull &&
      first.modifier !== CriterionModifier.NotNull &&
      (first.value?.length ?? 0) > 0
    ) {
      valueString = valueToString(first.value);
    }

    const modifierString = ModifierCriterion.getModifierLabel(
      intl,
      first.modifier,
    );
    const opts = {
      criterion: first.field,
      modifierString,
      valueString,
      others: "",
    };

    if (this.value.length === 1) {
      messageID = "custom_fields.criteria_format_string";
    } else {
      messageID = "custom_fields.criteria_format_string_others";
      opts.others = (this.value.length - 1).toString();
    }

    return intl.formatMessage({ id: messageID }, opts);
  }

  public getValueLabel(intl: IntlShape, v: CustomFieldCriterionInput): string {
    let valueString = "";

    if (
      v.modifier !== CriterionModifier.IsNull &&
      v.modifier !== CriterionModifier.NotNull &&
      (v.value?.length ?? 0) > 0
    ) {
      valueString = valueToString(v.value);
    }

    const modifierString = ModifierCriterion.getModifierLabel(intl, v.modifier);
    const opts = {
      criterion: v.field,
      modifierString,
      valueString,
    };

    return intl.formatMessage(
      { id: "custom_fields.criteria_format_string" },
      opts,
    );
  }

  public toQueryParams(): Record<string, unknown> {
    const encodedCriterion = {
      type: this.criterionOption.type,
      value: this.value,
    };
    return encodedCriterion;
  }

  public fromDecodedParams(i: unknown): void {
    const criterion = i as { value: CustomFieldCriterionInput[] };
    this.value = cloneDeep(criterion.value);
  }

  public setFromSavedCriterion(input: CustomFieldCriterionInput[]): void {
    this.value = cloneDeep(input);
  }
}

// One (field, modifier, value) triple, intended to live as a single AST
// condition. Multiple custom-field filters are composed via the AST's
// standard AND/OR groups rather than this criterion's internal shape.
export class SingleCustomFieldCriterion extends Criterion {
  public field: string = "";
  public caseSensitive?: boolean = false;
  public modifier: CriterionModifier = CriterionModifier.Equals;
  public value: unknown[] = [];

  constructor() {
    super(SingleCustomFieldCriterionOption);
  }

  protected cloneValues() {
    this.value = structuredClone(this.value);
  }

  public get supportsCaseSensitivity() {
    return (
      typeof (this.value[0] ?? "") === "string" &&
      [
        CriterionModifier.Equals,
        CriterionModifier.NotEquals,
        CriterionModifier.Includes,
        CriterionModifier.Excludes,
        CriterionModifier.MatchesRegex,
        CriterionModifier.NotMatchesRegex,
      ].includes(this.modifier)
    );
  }

  public get matchesCase() {
    return (
      this.caseSensitive ??
      (this.modifier !== CriterionModifier.Includes &&
        this.modifier !== CriterionModifier.Excludes)
    );
  }

  public isValid(): boolean {
    if (!this.field) return false;
    if (
      this.modifier === CriterionModifier.IsNull ||
      this.modifier === CriterionModifier.NotNull
    ) {
      return true;
    }
    return this.value.length > 0;
  }

  // AST → GraphQL: serialize to a single-element array under custom_fields
  // (the schema's `custom_fields: [CustomFieldCriterionInput!]` is always
  // an array).
  public toCriterionInputValue(): unknown {
    return [this.toCustomFieldInput()];
  }

  private toCustomFieldInput(): CustomFieldCriterionInput {
    return {
      field: this.field,
      modifier: this.modifier,
      value: cloneDeep(this.value),
      ...(this.caseSensitive !== undefined
        ? { case_sensitive: this.caseSensitive }
        : {}),
    };
  }

  public getLabel(intl: IntlShape): string {
    if (!this.field) return "";

    let valueString = "";
    if (
      this.modifier !== CriterionModifier.IsNull &&
      this.modifier !== CriterionModifier.NotNull &&
      this.value.length > 0
    ) {
      valueString = valueToString(this.value);
    }

    const modifierString = ModifierCriterion.getModifierLabel(
      intl,
      this.modifier,
    );

    const label = intl.formatMessage(
      { id: "custom_fields.criteria_format_string" },
      {
        criterion: this.field,
        modifierString,
        valueString,
      },
    );
    if (!this.supportsCaseSensitivity || this.caseSensitive === undefined)
      return label;
    return intl.formatMessage(
      {
        id: "search_filter.case_label",
        defaultMessage: "{filter} ({matching})",
      },
      {
        filter: label,
        matching: intl.formatMessage(
          this.caseSensitive
            ? {
                id: "search_filter.case_sensitive",
                defaultMessage: "Case sensitive",
              }
            : {
                id: "search_filter.case_insensitive",
                defaultMessage: "Ignore case",
              },
        ),
      },
    );
  }

  public toQueryParams(): Record<string, unknown> {
    return {
      type: this.criterionOption.type,
      field: this.field,
      modifier: this.modifier,
      value: this.value,
      ...(this.caseSensitive !== undefined
        ? { case_sensitive: this.caseSensitive }
        : {}),
    };
  }

  public fromDecodedParams(i: unknown): void {
    const decoded = i as {
      field?: string;
      modifier?: CriterionModifier;
      value?: unknown[];
      case_sensitive?: boolean;
    };
    if (
      decoded.case_sensitive !== undefined &&
      typeof decoded.case_sensitive !== "boolean"
    ) {
      throw new Error("case_sensitive must be a boolean");
    }
    this.caseSensitive = decoded.case_sensitive;
    this.field = decoded.field ?? "";
    this.modifier = decoded.modifier ?? CriterionModifier.Equals;
    this.value = cloneDeep(decoded.value ?? []);
  }

  public setFromSavedCriterion(input: CustomFieldCriterionInput): void {
    this.caseSensitive = input.case_sensitive ?? undefined;
    this.field = input.field;
    this.modifier = input.modifier;
    this.value = cloneDeep(input.value ?? []);
  }
}
