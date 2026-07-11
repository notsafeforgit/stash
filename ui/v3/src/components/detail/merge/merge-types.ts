/**
 * Per-field merge resolution for scene / performer merge dialogs.
 *
 * Why this exists: the backend's `sceneMerge` / `performerMerge`
 * mutations merge the source entities and apply the supplied
 * `values: <Type>UpdateInput` as partial destination overrides. Anything
 * not in `values` keeps whatever the destination already has — source
 * data is silently lost otherwise. So for a useful merge the client has
 * to compute the right `values` payload from per-field user choices.
 *
 * Choice shape: `"keep"` | `"source:<id>"` | `"combine"`. Stored as a
 * plain string so it slots into TanStack Form fields and the
 * `<ToggleGroup>` value model without further wrapping. The
 * `"source:<id>"` form encodes which source the value should come
 * from in bulk merges (single-source merges only ever produce
 * `"source:<the one source>"`).
 */
import type React from "react";

export type MergeChoice = "keep" | `source:${string}` | "combine";

export interface MergeFieldDef<TEntity, TUpdateInput, TValue = unknown> {
  /** Stable key used for the form field name and React key. */
  key: string;
  /** intl message id for the field label. */
  labelId: string;
  /** Fallback string if the locale lookup misses. */
  defaultLabel: string;

  /** Read the raw value from an entity (destination or any source). */
  read(entity: TEntity): TValue;

  /** "Empty" means absent for conflict-detection purposes (no chip shown). */
  isEmpty(value: TValue): boolean;

  /** Equality test — used to suppress rows where dest and source agree. */
  isEqual(a: TValue, b: TValue): boolean;

  /**
   * Render a value for the user-facing preview row. Receives the raw
   * value; should fall back to a muted "—" indicator when empty.
   */
  preview(value: TValue): React.ReactNode;

  /**
   * Combine multiple non-empty values into one — only present for
   * collection-shaped fields (tags, performers, urls, etc). The
   * three-way `<ToggleGroup>` only surfaces "Combine" when this is set.
   */
  combine?(values: TValue[]): TValue;

  /**
   * Apply the resolved value into the partial update input. Mutating
   * style (rather than returning a new object) so callers can compose
   * many field projections into a single accumulating input. Called
   * for "Use source N" choices and for "Combine"; never called for
   * "Keep" (the backend keeps the destination value automatically when
   * the field is absent from the update input).
   */
  toUpdate(input: TUpdateInput, value: TValue): void;
}

// Array element type — TValue is internal to each def, so we use
// `unknown` at the array level. Helper below creates a def with
// inferred TValue while presenting as `MergeFieldDef<E, U, unknown>`
// to the array.
export type AnyMergeFieldDef<TEntity, TUpdateInput> = MergeFieldDef<
  TEntity,
  TUpdateInput,
  unknown
>;

/** Type-preserving constructor for a merge field def. Lets callers
 *  write strongly-typed `read` / `preview` / `toUpdate` callbacks
 *  while the resulting object goes into a heterogeneous array. */
export function defineMergeField<TEntity, TUpdateInput, TValue>(
  def: MergeFieldDef<TEntity, TUpdateInput, TValue>,
): AnyMergeFieldDef<TEntity, TUpdateInput> {
  return def as unknown as AnyMergeFieldDef<TEntity, TUpdateInput>;
}
