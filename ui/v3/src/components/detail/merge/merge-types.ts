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
   * many field projections into a single accumulating input. Always called
   * for "Use source N" and "Combine"; callers using a safe merge contract
   * may also request projection for "Keep" as an explicit acknowledgement.
   */
  toUpdate(input: TUpdateInput, value: TValue): void;
}

export interface SourceRef<TEntity> {
  id: string;
  entity: TEntity;
  label: string;
}

export interface MergeRow<TUpdateInput> {
  field: { key: string; labelId: string; defaultLabel: string };
  sources: { id: string; label: string }[];
  canCombine: boolean;
  defaultChoice: MergeChoice;
  preview(choice: MergeChoice): React.ReactNode;
  apply(input: TUpdateInput, choice: MergeChoice): void;
}

/** The heterogeneous collection exposes operations, never an untyped value.
 * Each field keeps TValue inside its closure for both preview and projection. */
export interface AnyMergeFieldDef<TEntity, TUpdateInput> {
  readonly key: string;
  resolve(
    destination: TEntity,
    sources: readonly SourceRef<TEntity>[],
    projectKeepValues: boolean,
  ): MergeRow<TUpdateInput> | undefined;
}

export function defineMergeField<TEntity, TUpdateInput, TValue>(
  def: MergeFieldDef<TEntity, TUpdateInput, TValue>,
): AnyMergeFieldDef<TEntity, TUpdateInput> {
  return {
    key: def.key,
    resolve(destination, sources, projectKeepValues) {
      const destValue = def.read(destination);
      const destEmpty = def.isEmpty(destValue);
      const contributing = sources
        .map((source) => ({ ...source, value: def.read(source.entity) }))
        .filter((source) => !def.isEmpty(source.value));
      const first = contributing[0];
      if (
        !first ||
        (!destEmpty &&
          contributing.every((source) => def.isEqual(destValue, source.value)))
      )
        return undefined;
      const defaultChoice: MergeChoice = def.combine
        ? "combine"
        : destEmpty
          ? `source:${first.id}`
          : "keep";
      const sourceById = new Map(
        contributing.map((source) => [source.id, source]),
      );
      function resolveValue(choice: MergeChoice): TValue {
        if (choice === "combine" && def.combine) {
          const values = contributing.map((source) => source.value);
          if (!destEmpty) values.unshift(destValue);
          return def.combine(values);
        }
        if (choice.startsWith("source:")) {
          const source = sourceById.get(choice.slice("source:".length));
          return source ? source.value : destValue;
        }
        return destValue;
      }
      return {
        field: {
          key: def.key,
          labelId: def.labelId,
          defaultLabel: def.defaultLabel,
        },
        sources: contributing.map(({ id, label }) => ({ id, label })),
        canCombine: !!def.combine,
        defaultChoice,
        preview: (choice) => def.preview(resolveValue(choice)),
        apply(input, choice) {
          if (choice === "keep" && !projectKeepValues) return;
          if (choice === "combine" && !def.combine) return;
          if (
            choice.startsWith("source:") &&
            !sourceById.has(choice.slice("source:".length))
          )
            return;
          def.toUpdate(input, resolveValue(choice));
        },
      };
    },
  };
}
