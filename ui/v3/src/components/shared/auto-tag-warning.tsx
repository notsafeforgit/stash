import { FormattedMessage } from "react-intl";
import { AlertTriangle } from "lucide-react";

/**
 * Multi-paragraph explanation of what running auto-tag will do, with a
 * destructive-action warning. Re-used by every auto-tag entry point —
 * settings task, selective dialog, and per-entity confirm — so users see
 * the same context regardless of where they trigger it.
 *
 * Mirrors v2.5's Shared/AutoTagConfirmDialog AutoTagWarning.
 */
export function AutoTagWarning() {
  return (
    <div className="flex flex-col gap-2 text-sm">
      <p>
        <FormattedMessage
          id="config.tasks.auto_tag_based_on_filenames"
          defaultMessage="Auto tag content based on file paths."
        />
      </p>
      <p className="text-muted-foreground">
        <FormattedMessage
          id="config.tasks.auto_tag_confirm"
          defaultMessage="This will attempt to match your content against existing metadata."
        />
      </p>
      <p className="flex items-start gap-2 text-amber-600 dark:text-amber-400">
        <AlertTriangle className="mt-0.5 size-4 shrink-0" />
        <span>
          <FormattedMessage
            id="config.tasks.auto_tag_warning"
            defaultMessage="This process cannot be undone and may produce incorrect matches."
          />
        </span>
      </p>
    </div>
  );
}
