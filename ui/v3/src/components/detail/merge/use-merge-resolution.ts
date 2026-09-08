/**
 * Compute the per-field conflict rows and the resolved-update
 * projector for a merge dialog. Used by both scene and performer
 * merge dialogs once the user has picked a destination and the full
 * data for destination + sources has loaded.
 *
 * Conflict rule: a row is surfaced when at least one source has a
 * non-empty value. If the destination is also empty we default to
 * "Source" (no real conflict — the user gets the source data without
 * being asked to acknowledge it). If the destination is non-empty
 * and at least one source disagrees we default to "Keep" (force the
 * user to acknowledge before overwriting). For collection fields
 * (`combine` set on the field def) the default is "Combine" — losing
 * source tags / performers silently is the failure mode the whole
 * dialog exists to prevent.
 *
 * Output: `rows` to render, plus `applyResolutions(input, choices)`
 * which mutates the supplied partial-update input by walking each
 * row's chosen value through the field def's `toUpdate`. Choices are
 * stored externally (in the form) so this hook stays pure relative
 * to choice state.
 */
import { useMemo } from "react";
import type {
  AnyMergeFieldDef,
  MergeChoice,
  MergeRow,
  SourceRef,
} from "./merge-types";
export type { MergeRow, SourceRef } from "./merge-types";

export function useMergeResolution<TEntity, TUpdateInput>({
  fields,
  destination,
  sources,
  projectKeepValues = false,
}: {
  fields: readonly AnyMergeFieldDef<TEntity, TUpdateInput>[];
  destination: TEntity | null;
  sources: readonly SourceRef<TEntity>[];
  projectKeepValues?: boolean;
}) {
  return useMemo(() => {
    const rows: MergeRow<TUpdateInput>[] = destination
      ? fields.flatMap((field) => {
          const row = field.resolve(destination, sources, projectKeepValues);
          return row ? [row] : [];
        })
      : [];
    const defaultChoices: Record<string, MergeChoice> = Object.fromEntries(
      rows.map((row) => [row.field.key, row.defaultChoice]),
    );
    return {
      rows,
      defaultChoices,
      applyResolutions(
        input: TUpdateInput,
        choices: Record<string, MergeChoice>,
      ): void {
        for (const row of rows)
          row.apply(input, choices[row.field.key] ?? row.defaultChoice);
      },
    };
  }, [fields, destination, sources, projectKeepValues]);
}
