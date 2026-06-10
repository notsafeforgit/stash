/**
 * Resolution panel surfaced inside scene / performer merge dialogs
 * once a destination has been picked. Renders one `<MergeFieldRow>`
 * per conflicting field. The choice state is owned by the parent
 * dialog (kept outside the form because the choice values are
 * always valid by construction — validation has nothing to add).
 */
import type React from "react";
import { useIntl } from "react-intl";
import { CheckCircle2 } from "lucide-react";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "src/components/ui/empty";
import { MergeFieldRow, type MergeRowSourceOption } from "./merge-field-row";
import type { MergeChoice } from "./merge-types";
import type { MergeRow, SourceRef } from "./use-merge-resolution";

interface MergeResolutionPanelProps<TEntity, TUpdateInput> {
  rows: MergeRow<TEntity, TUpdateInput>[];
  /** All sources passed to the resolution hook — used to look up
   *  per-source labels for the toggle buttons. */
  sources: readonly SourceRef<TEntity>[];
  /** Map of fieldKey → current choice. */
  choices: Record<string, MergeChoice>;
  /** Updates a single field's choice. */
  onChoiceChange: (fieldKey: string, next: MergeChoice) => void;
}

export function MergeResolutionPanel<TEntity, TUpdateInput>({
  rows,
  sources,
  choices,
  onChoiceChange,
}: MergeResolutionPanelProps<TEntity, TUpdateInput>) {
  const intl = useIntl();

  if (rows.length === 0) {
    return (
      <Empty className="border border-dashed border-border rounded-md py-4">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <CheckCircle2 />
          </EmptyMedia>
          <EmptyTitle>
            {intl.formatMessage({
              id: "dialogs.merge.no_conflicts_title",
              defaultMessage: "Nothing to resolve",
            })}
          </EmptyTitle>
          <EmptyDescription>
            {intl.formatMessage({
              id: "dialogs.merge.no_conflicts_desc",
              defaultMessage:
                "The destination already has all the values it needs.",
            })}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="text-xs uppercase tracking-wide text-muted-foreground font-semibold">
        {intl.formatMessage({
          id: "dialogs.merge.resolve_section",
          defaultMessage: "Resolve fields",
        })}
      </div>
      {rows.map((row) => (
        <MergeRowBody
          key={row.field.key}
          row={row}
          sources={sources}
          choice={choices[row.field.key] ?? row.defaultChoice}
          onChange={(next) => onChoiceChange(row.field.key, next)}
          fieldLabel={intl.formatMessage({
            id: row.field.labelId,
            defaultMessage: row.field.defaultLabel,
          })}
        />
      ))}
    </div>
  );
}

function MergeRowBody<TEntity, TUpdateInput>({
  row,
  sources,
  choice,
  onChange,
  fieldLabel,
}: {
  row: MergeRow<TEntity, TUpdateInput>;
  sources: readonly SourceRef<TEntity>[];
  choice: MergeChoice;
  onChange: (next: MergeChoice) => void;
  fieldLabel: string;
}) {
  // Toggle options for the contributing sources only. Source order
  // mirrors the row's `sources` list (which itself preserves the
  // dialog's source order).
  const sourceById = new Map(sources.map((s) => [s.id, s]));
  const toggleSources: MergeRowSourceOption[] = row.sources
    .map((s) => {
      const ref = sources.find((src) => src.entity === s.entity);
      return ref ? { id: ref.id, label: ref.label } : null;
    })
    .filter((x): x is MergeRowSourceOption => x !== null);

  // Live preview: mirror the resolution math in `applyResolutions`
  // so the on-screen preview always matches the value the merge
  // will produce.
  const resolvedPreview: React.ReactNode = (() => {
    if (choice === "keep") return row.field.preview(row.destValue);
    if (choice === "combine" && row.field.combine) {
      const all: unknown[] = [];
      if (!row.field.isEmpty(row.destValue)) all.push(row.destValue);
      for (const src of row.sources) all.push(src.value);
      return row.field.preview(row.field.combine(all));
    }
    if (typeof choice === "string" && choice.startsWith("source:")) {
      const id = choice.slice("source:".length);
      const sourceRef = sourceById.get(id);
      if (!sourceRef) return row.field.preview(row.destValue);
      const value = row.field.read(sourceRef.entity);
      return row.field.preview(value);
    }
    return row.field.preview(row.destValue);
  })();

  return (
    <MergeFieldRow
      label={fieldLabel}
      htmlId={`merge-row-${row.field.key}`}
      value={choice}
      onChange={onChange}
      sources={toggleSources}
      canCombine={!!row.field.combine}
      resolvedPreview={resolvedPreview}
    />
  );
}
