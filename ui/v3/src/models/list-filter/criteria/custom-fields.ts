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

// Legacy multi-condition custom_fields criterion. Kept as `hidden` so that
// saved filters / URLs encoded against v2.5 (or early v3) decode without
// error, but it's no longer offered in any picker — the v3 UI uses
// SingleCustomFieldCriterion (one field per AST condition, combined via the
// AST's standard AND/OR groups).
export const CustomFieldsCriterionOption = new CriterionOption({
  type: "custom_fields",
  messageID: "custom_fields.title",
  makeCriterion: () => new CustomFieldsCriterion(),
  hidden: true,
});

// Single-field custom_fields criterion. One AST condition holds exactly one
// (field, modifier, value) triple. Server-side it serializes into the same
// `custom_fields` filter slot as the legacy form (via serverField below) —
// when multiple of these appear at the same AST level, the merger
// (pkg/models/filter_ast.go:groupChildrenToObjectFilter) splits them into
// `{custom_fields: [...], AND/OR: {custom_fields: [...]}}` so the server
// applies each independently.
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

  public applyToCriterionInput(input: Record<string, unknown>): void {
    input.custom_fields = cloneDeep(this.value);
  }

  public applyToSavedCriterion(input: Record<string, unknown>): void {
    input.custom_fields = cloneDeep(this.value);
  }

  // AST → GraphQL serialization. The server's FilterAST condition shape is
  // `{field, value: Any}`; for a custom_fields condition the value is the
  // full array of CustomFieldCriterionInput, which the server unwraps into
  // `custom_fields` on the underlying object filter.
  public toCriterionInputValue(): unknown {
    return cloneDeep(this.value);
  }

  public getLabel(intl: IntlShape): string {
    // show first criterion
    if (this.value.length === 0) {
      return "";
    }

    const first = this.value[0];
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
  public modifier: CriterionModifier = CriterionModifier.Equals;
  public value: unknown[] = [];

  constructor() {
    super(SingleCustomFieldCriterionOption);
  }

  protected cloneValues() {
    this.value = structuredClone(this.value);
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

  public applyToCriterionInput(input: Record<string, unknown>): void {
    const entry = this.toCustomFieldInput();
    const existing = input.custom_fields as
      | CustomFieldCriterionInput[]
      | undefined;
    input.custom_fields = existing ? [...existing, entry] : [entry];
  }

  public applyToSavedCriterion(input: Record<string, unknown>): void {
    this.applyToCriterionInput(input);
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

    return intl.formatMessage(
      { id: "custom_fields.criteria_format_string" },
      {
        criterion: this.field,
        modifierString,
        valueString,
      },
    );
  }

  public toQueryParams(): Record<string, unknown> {
    return {
      type: this.criterionOption.type,
      field: this.field,
      modifier: this.modifier,
      value: this.value,
    };
  }

  public fromDecodedParams(i: unknown): void {
    const decoded = i as {
      field?: string;
      modifier?: CriterionModifier;
      value?: unknown[];
    };
    this.field = decoded.field ?? "";
    this.modifier = decoded.modifier ?? CriterionModifier.Equals;
    this.value = cloneDeep(decoded.value ?? []);
  }

  public setFromSavedCriterion(input: CustomFieldCriterionInput): void {
    this.field = input.field;
    this.modifier = input.modifier;
    this.value = cloneDeep(input.value ?? []);
  }
}
