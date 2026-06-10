import type { Criterion } from "./criteria/criterion";
import type { CriterionType, SavedASTNode } from "./types";
import {
  CriterionModifier,
  FilterGroupOperator,
  type FilterMode,
} from "src/core/generated-graphql";
import type { IntlShape } from "react-intl";
import { getFilterOptions } from "./factory";

export type FilterASTNode = FilterASTGroupNode | FilterASTConditionNode;

export type FilterASTGroupNode = {
  kind: "group";
  id: string;
  operator: FilterGroupOperator;
  children: FilterASTNode[];
};

export type FilterASTConditionNode = {
  kind: "condition";
  id: string;
  field: CriterionType;
  criterion: Criterion;
};

// ── Compact encoded shape ─────────────────────────────────────────────────
//
// Used both in URLs (`fa=` is base64 of this JSON) and saved filters
// (`__filter_ast` key).
//
//   Group:     { k: 0, o: <op-int>, c: [child, ...] }
//   Condition: { k: 1, f: <field>, m: <mod-int>, v?: <value> }
//
// The criterion's redundant `type` is dropped (it always equals `f`) and
// the `{type, modifier, value}` wrapper is flattened into the condition.
// Operator/modifier index lists are append-only — never reorder.
const COMPACT_GROUP = 0;
const COMPACT_CONDITION = 1;

const COMPACT_OPERATORS: readonly FilterGroupOperator[] = [
  FilterGroupOperator.And,
  FilterGroupOperator.Or,
];

const COMPACT_MODIFIERS: readonly CriterionModifier[] = [
  CriterionModifier.Equals,
  CriterionModifier.NotEquals,
  CriterionModifier.GreaterThan,
  CriterionModifier.LessThan,
  CriterionModifier.IsNull,
  CriterionModifier.NotNull,
  CriterionModifier.Includes,
  CriterionModifier.IncludesAll,
  CriterionModifier.Excludes,
  CriterionModifier.MatchesRegex,
  CriterionModifier.NotMatchesRegex,
  CriterionModifier.Between,
  CriterionModifier.NotBetween,
];

const OPERATOR_TO_INT = new Map(
  COMPACT_OPERATORS.map((op, i) => [op, i] as const),
);
const MODIFIER_TO_INT = new Map(
  COMPACT_MODIFIERS.map((mod, i) => [mod, i] as const),
);

interface CompactGroupNode {
  k: typeof COMPACT_GROUP;
  o: number;
  c: CompactASTNode[];
}

interface CompactConditionNode {
  k: typeof COMPACT_CONDITION;
  f: CriterionType;
  // Modifier is omitted for criteria that aren't a single (modifier, value)
  // pair — e.g. the legacy multi-condition custom_fields, which carries an
  // array of independently-modified sub-conditions and exposes no top-level
  // modifier of its own.
  m?: number;
  v?: unknown;
  // Custom-field name. Only set for the single-field custom_fields criterion,
  // which needs to round-trip a field name alongside modifier/value.
  cf?: string;
}

type CompactASTNode = CompactGroupNode | CompactConditionNode;

export type EncodedFilterASTNode = CompactASTNode;

function makeID() {
  return Math.random().toString(36).slice(2, 10);
}

export function astCriterionOptions(mode: FilterMode) {
  return getFilterOptions(mode).criterionOptions.filter(
    (option) => !option.hidden,
  );
}

export function astSupportsCriterionType(
  mode: FilterMode,
  type: CriterionType,
) {
  return astCriterionOptions(mode).some((option) => option.type === type);
}

export function createASTCondition(
  mode: FilterMode,
  type?: CriterionType,
): FilterASTConditionNode {
  const options = astCriterionOptions(mode);
  const resolvedType = type ?? options[0]?.type;
  const option = options.find((o) => o.type === resolvedType);
  if (!option) {
    throw new Error(
      `unsupported AST criterion type ${resolvedType} for mode ${mode}`,
    );
  }

  return {
    kind: "condition",
    id: makeID(),
    field: option.type,
    criterion: option.makeCriterion(),
  };
}

export function createASTConditionFromCriterion(
  mode: FilterMode,
  criterion: Criterion,
): FilterASTConditionNode {
  const { type } = criterion.criterionOption;

  if (!astSupportsCriterionType(mode, type)) {
    throw new Error(`unsupported AST criterion type ${type} for mode ${mode}`);
  }

  return createConditionNode(criterion);
}

/**
 * Wrap a criterion in a condition node without the builder-support check.
 * Used when folding non-builder criteria (custom_fields etc.) into the
 * persisted AST; `splitNonBuilderConditions` in filter.ts is the inverse.
 */
export function createConditionNode(
  criterion: Criterion,
): FilterASTConditionNode {
  return {
    kind: "condition",
    id: makeID(),
    field: criterion.criterionOption.type,
    criterion: criterion.clone(),
  };
}

export function createASTGroup(
  mode: FilterMode,
  operator: FilterGroupOperator = FilterGroupOperator.And,
  children?: FilterASTNode[],
): FilterASTGroupNode {
  return {
    kind: "group",
    id: makeID(),
    operator,
    children: children ?? [createASTCondition(mode)],
  };
}

export function cloneFilterASTNode(node: FilterASTNode): FilterASTNode {
  if (node.kind === "condition") {
    return {
      kind: "condition",
      id: node.id,
      field: node.field,
      criterion: node.criterion.clone(),
    };
  }

  return {
    kind: "group",
    id: node.id,
    operator: node.operator,
    children: node.children.map(cloneFilterASTNode),
  };
}

export function countFilterASTConditions(node?: FilterASTNode): number {
  if (!node) {
    return 0;
  }

  if (node.kind === "condition") {
    return 1;
  }

  return node.children.reduce(
    (total, child) => total + countFilterASTConditions(child),
    0,
  );
}

export function encodeFilterASTNode(node: FilterASTNode): CompactASTNode {
  if (node.kind === "condition") {
    const params = node.criterion.toQueryParams();
    const out: CompactConditionNode = {
      k: COMPACT_CONDITION,
      f: node.field,
    };
    // Some criteria (e.g. legacy multi-condition custom_fields) don't expose
    // a top-level modifier — their value carries per-entry modifiers
    // internally. Omit `m` for those.
    if (params.modifier !== undefined) {
      const modifier = params.modifier as CriterionModifier;
      const m = MODIFIER_TO_INT.get(modifier);
      if (m === undefined) {
        throw new Error(`unknown criterion modifier: ${modifier}`);
      }
      out.m = m;
    }
    if ("value" in params) {
      out.v = params.value;
    }
    if (typeof params.field === "string" && params.field !== "") {
      out.cf = params.field;
    }
    return out;
  }

  const o = OPERATOR_TO_INT.get(node.operator);
  if (o === undefined) {
    throw new Error(`unknown filter group operator: ${node.operator}`);
  }
  return {
    k: COMPACT_GROUP,
    o,
    c: node.children.map(encodeFilterASTNode),
  };
}

export function decodeFilterASTNode(
  mode: FilterMode,
  input: unknown,
): FilterASTNode {
  if (typeof input !== "object" || input === null) {
    throw new Error("invalid encoded filter AST node");
  }

  const compact = input as CompactASTNode;
  if (compact.k === COMPACT_CONDITION) {
    const option = astCriterionOptions(mode).find((o) => o.type === compact.f);
    if (!option) {
      throw new Error(
        `unsupported AST criterion type ${compact.f} for mode ${mode}`,
      );
    }
    let modifier: CriterionModifier | undefined;
    if (compact.m !== undefined) {
      modifier = COMPACT_MODIFIERS[compact.m];
      if (modifier === undefined) {
        throw new Error(`unknown criterion modifier index: ${compact.m}`);
      }
    }
    const criterion = option.makeCriterion();
    criterion.fromDecodedParams({
      modifier,
      value: compact.v,
      field: compact.cf,
    });
    return {
      kind: "condition",
      id: makeID(),
      field: compact.f,
      criterion,
    };
  }

  if (compact.k === COMPACT_GROUP) {
    const operator = COMPACT_OPERATORS[compact.o];
    if (operator === undefined) {
      throw new Error(`unknown filter group operator index: ${compact.o}`);
    }
    return {
      kind: "group",
      id: makeID(),
      operator,
      children: compact.c.map((child) => decodeFilterASTNode(mode, child)),
    };
  }

  throw new Error(
    `unknown encoded filter AST node kind: ${(compact as { k: unknown }).k}`,
  );
}

export function filterASTNodeToGraphQL(
  node: FilterASTNode,
): Record<string, unknown> {
  if (node.kind === "condition") {
    const criterion = node.criterion as Criterion & {
      toCriterionInputValue?: () => unknown;
    };
    const value =
      typeof criterion.toCriterionInputValue === "function"
        ? criterion.toCriterionInputValue()
        : undefined;

    // The server filter slot may differ from the client criterion type — see
    // CriterionOption.serverField. Default to the criterion's type.
    const serverField =
      node.criterion.criterionOption.serverField ?? node.field;

    return {
      condition: {
        field: serverField,
        value,
      },
    };
  }

  return {
    group: {
      operator: node.operator,
      children: node.children.map(filterASTNodeToGraphQL),
    },
  };
}

// ── Persisted (saved-filter) shape ──────────────────────────────────────────
//
// SavedFilter.filter_ast stores the canonical node tree as
// `{group: {operator, children}} | {condition: {field, value}}` — the same
// structure as FilterASTNodeInput. `field` is the client criterion type and
// `value` is the labeled saved-criterion shape (`{modifier?, value?, field?}`)
// that `applyToSavedCriterion` historically wrote into object_filter; the
// backend flattens these values verbatim into the v2.5 compatibility view.

export function encodeFilterASTNodeToSaved(node: FilterASTNode): SavedASTNode {
  if (node.kind === "condition") {
    const params = node.criterion.toQueryParams();
    const value: Record<string, unknown> = {};
    if (params.modifier !== undefined) {
      value.modifier = params.modifier;
    }
    if ("value" in params) {
      value.value = params.value;
    }
    if (typeof params.field === "string" && params.field !== "") {
      value.field = params.field;
    }
    return { condition: { field: node.field, value } };
  }

  return {
    group: {
      operator: node.operator,
      children: node.children.map(encodeFilterASTNodeToSaved),
    },
  };
}

const SAVED_OPERATORS: readonly FilterGroupOperator[] = [
  FilterGroupOperator.And,
  FilterGroupOperator.Or,
];

export function decodeSavedFilterASTNode(
  raw: unknown,
  makeCriterion: (type: CriterionType) => Criterion,
): FilterASTNode {
  if (!raw || typeof raw !== "object") {
    throw new Error("invalid saved filter AST node");
  }

  const node = raw as {
    condition?: { field?: unknown; value?: unknown };
    group?: { operator?: unknown; children?: unknown };
  };

  if (node.condition) {
    const { field, value } = node.condition;
    if (typeof field !== "string" || field === "") {
      throw new Error("saved filter AST condition must have a field");
    }

    const criterion = makeCriterion(field as CriterionType);
    const v = (value ?? {}) as {
      modifier?: unknown;
      value?: unknown;
      field?: unknown;
    };
    criterion.fromDecodedParams({
      modifier: v.modifier,
      value: v.value,
      field: v.field,
    });

    return {
      kind: "condition",
      id: makeID(),
      field: field as CriterionType,
      criterion,
    };
  }

  if (node.group) {
    const { operator, children } = node.group;
    if (!SAVED_OPERATORS.includes(operator as FilterGroupOperator)) {
      throw new Error(`unknown saved filter AST operator: ${operator}`);
    }
    if (!Array.isArray(children)) {
      throw new Error("saved filter AST group children must be an array");
    }
    return {
      kind: "group",
      id: makeID(),
      operator: operator as FilterGroupOperator,
      children: children.map((child) =>
        decodeSavedFilterASTNode(child, makeCriterion),
      ),
    };
  }

  throw new Error("saved filter AST node must contain group or condition");
}

export function filterASTConditionLabel(
  node: FilterASTConditionNode,
  intl: IntlShape,
  sfwContentMode: boolean,
) {
  return node.criterion.getLabel(intl, sfwContentMode);
}

export function flattenFilterASTConditions(
  node?: FilterASTNode,
): FilterASTConditionNode[] {
  if (!node) {
    return [];
  }

  if (node.kind === "condition") {
    return [node];
  }

  return node.children.flatMap(flattenFilterASTConditions);
}
