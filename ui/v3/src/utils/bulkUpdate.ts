import type * as GQL from "src/core/generated-graphql";

/** Stable JSON-based deep equality for plain objects/arrays. */
function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (Array.isArray(a) && Array.isArray(b)) {
    if (a.length !== b.length) return false;
    return a.every((v, i) => deepEqual(v, b[i]));
  }
  if (
    a !== null &&
    b !== null &&
    typeof a === "object" &&
    typeof b === "object"
  ) {
    const ka = Object.keys(a as object).sort();
    const kb = Object.keys(b as object).sort();
    if (!deepEqual(ka, kb)) return false;
    return ka.every((k) =>
      deepEqual(
        (a as Record<string, unknown>)[k],
        (b as Record<string, unknown>)[k],
      ),
    );
  }
  return false;
}

/**
 * Returns `newValue` when `first === true`, or when `currentValue` equals
 * `newValue`. Returns `undefined` (= indeterminate / no common value) when
 * successive items have different values.
 */
export function getAggregateState<T>(
  currentValue: T | undefined,
  newValue: T,
  first: boolean,
): T | undefined {
  if (!first && !deepEqual(currentValue, newValue)) return undefined;
  return newValue;
}

/**
 * Applies `getAggregateState` across multiple fields of a plain object,
 * mutating `output` in place.
 */
export function getAggregateStateObject<O extends object, I extends object>(
  output: O,
  input: I,
  fields: (string & keyof O & keyof I)[],
  first: boolean,
) {
  for (const key of fields) {
    (output as Record<string, unknown>)[key] = getAggregateState(
      (output as Record<string, unknown>)[key] as O[typeof key],
      (input as Record<string, unknown>)[key] as O[typeof key],
      first,
    );
  }
}

/**
 * Reduces lists of sorted IDs to the common set.
 * Returns `[]` when items disagree (indeterminate).
 */
export function getAggregateIds(sortedLists: string[][]): string[] {
  let result: string[] = [];
  let first = true;
  for (const list of sortedLists) {
    if (first) {
      result = list;
      first = false;
    } else if (!deepEqual(result, list)) {
      return [];
    }
  }
  return result;
}

export function makeBulkUpdateIds(
  ids: string[],
  mode: GQL.BulkUpdateIdMode,
): GQL.BulkUpdateIds {
  return { mode, ids };
}

/**
 * IDs present on every list — i.e. the entities being bulk-edited all
 * have these in common. Used to filter Add-mode dropdown options (adding
 * an item already on every entity is a no-op).
 */
export function getIntersectionIds(lists: string[][]): string[] {
  if (lists.length === 0) return [];
  const [first, ...rest] = lists;
  if (rest.length === 0) return [...first];
  const sets = rest.map((l) => new Set(l));
  return first.filter((id) => sets.every((s) => s.has(id)));
}

/**
 * IDs present on at least one list — i.e. the entities being bulk-edited
 * collectively have these. Used to filter Remove-mode dropdown options
 * (removing an item not on any entity is a no-op).
 */
export function getUnionIds(lists: string[][]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const list of lists) {
    for (const id of list) {
      if (!seen.has(id)) {
        seen.add(id);
        out.push(id);
      }
    }
  }
  return out;
}
