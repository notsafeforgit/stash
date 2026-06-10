/**
 * Single conflict-row in the merge dialog. Three-way (Keep / Use
 * source / Combine) toggle on top, live-resolved value preview
 * underneath. Reused by both the scene and performer dialogs.
 *
 * The "Use source N" choices fan out into one toggle button per
 * source for bulk merges; for single-source merges they collapse to
 * one button labelled "Use source". "Combine" only appears when the
 * field def supplies a `combine` function (i.e. only collection
 * fields — tags, performers, urls, etc.).
 */
import type React from "react";
import { useIntl } from "react-intl";
import { Field, FieldLabel } from "src/components/ui/field";
import { ToggleGroup, ToggleGroupItem } from "src/components/ui/toggle-group";
import { cn } from "src/lib/utils";
import type { MergeChoice } from "./merge-types";

export interface MergeRowSourceOption {
  /** Source entity id — used in the `source:<id>` choice value. */
  id: string;
  /** Short label shown on the toggle ("Source" for single-source,
   *  the source's title/name for bulk). */
  label: string;
}

export interface MergeFieldRowProps {
  /** Localised field label (e.g. "Title", "Tags"). */
  label: string;
  /** Stable id used for `<FieldLabel htmlFor>` accessibility. */
  htmlId: string;
  /** Current choice (form value). */
  value: MergeChoice;
  onChange: (next: MergeChoice) => void;
  /** Eligible source entities. Always non-empty when a row renders
   *  (rows are only built when there's at least one source value). */
  sources: MergeRowSourceOption[];
  /** Whether the field can be combined across sources + dest
   *  (collection fields). */
  canCombine: boolean;
  /** Preview of what the destination's value will be once the merge
   *  applies — recomputed by the parent based on `value`. */
  resolvedPreview: React.ReactNode;
}

export function MergeFieldRow({
  label,
  htmlId,
  value,
  onChange,
  sources,
  canCombine,
  resolvedPreview,
}: MergeFieldRowProps) {
  const intl = useIntl();
  const isSingleSource = sources.length === 1;
  return (
    <Field>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <FieldLabel htmlFor={htmlId} className="text-sm font-medium">
          {label}
        </FieldLabel>
        <ToggleGroup
          id={htmlId}
          variant="outline"
          size="sm"
          value={[value]}
          onValueChange={(arr) => {
            // ToggleGroup permits clearing its value by re-clicking the
            // active item, returning []. A merge row must always have
            // a chosen value — drop empty changes.
            const next = arr[0];
            if (!next) return;
            onChange(next as MergeChoice);
          }}
          className="shrink-0"
        >
          <ToggleGroupItem value="keep">
            {intl.formatMessage({
              id: "dialogs.merge.choice_keep",
              defaultMessage: "Keep",
            })}
          </ToggleGroupItem>
          {sources.map((src) => (
            <ToggleGroupItem
              key={src.id}
              value={`source:${src.id}` satisfies MergeChoice}
              title={src.label}
            >
              {isSingleSource
                ? intl.formatMessage({
                    id: "dialogs.merge.choice_source",
                    defaultMessage: "Source",
                  })
                : src.label}
            </ToggleGroupItem>
          ))}
          {canCombine && (
            <ToggleGroupItem value="combine">
              {intl.formatMessage({
                id: "dialogs.merge.choice_combine",
                defaultMessage: "Combine",
              })}
            </ToggleGroupItem>
          )}
        </ToggleGroup>
      </div>
      <div
        className={cn(
          "rounded-md border border-border/60 bg-muted/30 px-3 py-2",
          "text-sm min-w-0 break-words",
        )}
      >
        {resolvedPreview}
      </div>
    </Field>
  );
}

/** Standardised "no value" rendering used by every previewer. */
export function MergeEmptyPreview() {
  const intl = useIntl();
  return (
    <span className="italic text-muted-foreground">
      {intl.formatMessage({
        id: "dialogs.merge.preview_empty",
        defaultMessage: "(empty)",
      })}
    </span>
  );
}
