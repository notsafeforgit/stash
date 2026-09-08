/**
 * Resolution panel surfaced inside scene / performer merge dialogs
 * once a destination has been picked. Renders one `<MergeFieldRow>`
 * per conflicting field. The choice state is owned by the parent
 * dialog (kept outside the form because the choice values are
 * always valid by construction — validation has nothing to add).
 */
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
  rows: MergeRow<TUpdateInput>[];
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

function MergeRowBody<TUpdateInput>({
  row,
  choice,
  onChange,
  fieldLabel,
}: {
  row: MergeRow<TUpdateInput>;
  choice: MergeChoice;
  onChange: (next: MergeChoice) => void;
  fieldLabel: string;
}) {
  const toggleSources: MergeRowSourceOption[] = row.sources;
  const resolvedPreview = row.preview(choice);

  return (
    <MergeFieldRow
      label={fieldLabel}
      htmlId={`merge-row-${row.field.key}`}
      value={choice}
      onChange={onChange}
      sources={toggleSources}
      canCombine={row.canCombine}
      resolvedPreview={resolvedPreview}
    />
  );
}
