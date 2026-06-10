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
import type { AnyMergeFieldDef, MergeChoice } from "./merge-types";

export interface MergeRow<TEntity, TUpdateInput> {
  field: AnyMergeFieldDef<TEntity, TUpdateInput>;
  /** Per-source values, in the same order as `sources` was passed in.
   *  Sources whose value is empty are dropped — the row only carries
   *  sources that actually contribute something. */
  sources: Array<{ entity: TEntity; value: unknown }>;
  /** Destination value (may be "empty" — see `field.isEmpty`). */
  destValue: unknown;
  /** Default choice the form is initialised to. */
  defaultChoice: MergeChoice;
}

export interface SourceRef<TEntity> {
  id: string;
  entity: TEntity;
  /** Short user-facing label (e.g. scene title, performer name). Used
   *  on the per-source toggle button in bulk merges. */
  label: string;
}

interface UseMergeResolutionArgs<TEntity, TUpdateInput> {
  fields: readonly AnyMergeFieldDef<TEntity, TUpdateInput>[];
  destination: TEntity | null;
  sources: readonly SourceRef<TEntity>[];
}

export function useMergeResolution<TEntity, TUpdateInput>({
  fields,
  destination,
  sources,
}: UseMergeResolutionArgs<TEntity, TUpdateInput>): {
  rows: MergeRow<TEntity, TUpdateInput>[];
  defaultChoices: Record<string, MergeChoice>;
  applyResolutions(
    input: TUpdateInput,
    choices: Record<string, MergeChoice>,
  ): void;
} {
  // Both `rows` and `defaultChoices` are derived from the same walk
  // of the field defs against destination + sources, so they share
  // one memo. Recomputes whenever the inputs identity-change — the
  // dialog feeds new arrays after the destination picker resolves
  // and the data query lands, both of which are infrequent events.
  const { rows, defaultChoices } = useMemo(() => {
    if (!destination) {
      return { rows: [], defaultChoices: {} as Record<string, MergeChoice> };
    }
    const computedRows: MergeRow<TEntity, TUpdateInput>[] = [];
    const defaults: Record<string, MergeChoice> = {};
    for (const field of fields) {
      const destValue = field.read(destination);
      const destEmpty = field.isEmpty(destValue);
      const contributingSources = sources
        .map((src) => ({ ...src, value: field.read(src.entity) }))
        .filter((src) => !field.isEmpty(src.value));

      // No source contributes anything → no conflict, no row.
      if (contributingSources.length === 0) continue;

      // All sources agree with destination (and dest is non-empty)
      // → nothing to choose between. Skip.
      if (
        !destEmpty &&
        contributingSources.every((src) => field.isEqual(destValue, src.value))
      ) {
        continue;
      }

      // Default-choice rules — see header comment.
      let defaultChoice: MergeChoice;
      if (field.combine) {
        defaultChoice = "combine";
      } else if (destEmpty) {
        // Use the first contributing source's value.
        defaultChoice = `source:${contributingSources[0].id}`;
      } else {
        defaultChoice = "keep";
      }

      computedRows.push({
        field,
        destValue,
        sources: contributingSources.map((s) => ({
          entity: s.entity,
          value: s.value,
        })),
        defaultChoice,
      });
      defaults[field.key] = defaultChoice;
    }
    return { rows: computedRows, defaultChoices: defaults };
  }, [fields, destination, sources]);

  // applyResolutions itself is stable per input identity — same
  // memo deps so it shares the recomputation. The function closes
  // over `rows` and the source lookup so it can resolve a
  // `source:<id>` choice back to the right value.
  const applyResolutions = useMemo(() => {
    const sourceById = new Map<string, TEntity>(
      sources.map((s) => [s.id, s.entity]),
    );
    return (
      input: TUpdateInput,
      choices: Record<string, MergeChoice>,
    ): void => {
      for (const row of rows) {
        const choice = choices[row.field.key] ?? row.defaultChoice;
        if (choice === "keep") continue;
        if (choice === "combine") {
          if (!row.field.combine) continue;
          const allValues: unknown[] = [];
          if (!row.field.isEmpty(row.destValue)) allValues.push(row.destValue);
          for (const src of row.sources) allValues.push(src.value);
          if (allValues.length === 0) continue;
          row.field.toUpdate(input, row.field.combine(allValues));
          continue;
        }
        // choice = `source:<id>`
        const id = choice.slice("source:".length);
        const sourceEntity = sourceById.get(id);
        if (!sourceEntity) continue;
        const value = row.field.read(sourceEntity);
        if (row.field.isEmpty(value)) continue;
        row.field.toUpdate(input, value);
      }
    };
  }, [rows, sources]);

  return { rows, defaultChoices, applyResolutions };
}
