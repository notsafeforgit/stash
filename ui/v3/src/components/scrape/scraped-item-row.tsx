import type React from "react";
import { useIntl } from "react-intl";
import { Check, PlusCircle, X } from "lucide-react";
import { cn } from "src/lib/utils";
import { Button } from "src/components/ui/button";
import {
  type EntityOption,
  EntitySingleSelect,
} from "src/components/forms/async-entity-select";

/**
 * How the user wants to handle a scraped item at apply-time:
 *   - `skip` — drop it
 *   - `existing` — add an already-stored entity (auto-matched via stored_id
 *     or manually picked)
 *   - `create` — create a new entity with `name` and add it
 *
 * Used by every per-item scraped row component (tag, performer, studio,
 * group). Each entity-specific row file wraps this picker with the
 * appropriate Find query + intl labels.
 */
export type ScrapedItemResolution =
  | { kind: "skip" }
  | { kind: "existing"; option: EntityOption }
  | { kind: "create"; name: string };

export function defaultItemResolution(scraped: {
  stored_id?: string | null;
  name?: string | null;
}): ScrapedItemResolution {
  if (scraped.stored_id && scraped.name) {
    return {
      kind: "existing",
      option: { id: scraped.stored_id, name: scraped.name },
    };
  }
  if (scraped.name) {
    return { kind: "create", name: scraped.name };
  }
  return { kind: "skip" };
}

export interface ScrapedItemRowLabels {
  /** Tooltip on the "pick existing" button. */
  useExisting: string;
  /** Tooltip on the "create new" button. */
  createNew: string;
  /** Tooltip on the "skip" button. */
  skip: string;
  /** Caption shown next to the row when in `create` mode, given the name. */
  willCreate: (name: string) => string;
}

interface ScrapedItemRowProps {
  scraped: { stored_id?: string | null; name?: string | null };
  /** Optional secondary line under the name (e.g. performer disambiguation). */
  subtitle?: React.ReactNode;
  value: ScrapedItemResolution;
  onChange: (next: ScrapedItemResolution) => void;
  /** Search results for the EntitySingleSelect. */
  searchOptions: EntityOption[];
  onSearch: (query: string) => void;
  searching: boolean;
  labels: ScrapedItemRowLabels;
}

export function ScrapedItemRow({
  scraped,
  subtitle,
  value,
  onChange,
  searchOptions,
  onSearch,
  searching,
  labels,
}: ScrapedItemRowProps) {
  const intl = useIntl();
  const isExisting = value.kind === "existing";
  const isCreate = value.kind === "create";
  const isSkip = value.kind === "skip";

  function setKind(kind: ScrapedItemResolution["kind"]) {
    if (kind === "skip") {
      onChange({ kind: "skip" });
    } else if (kind === "create") {
      onChange({ kind: "create", name: scraped.name ?? "" });
    } else {
      onChange(defaultItemResolution(scraped));
    }
  }

  // No name to act on — render nothing rather than a confusing empty row.
  if (!scraped.name) return null;

  return (
    <div
      className={cn(
        "flex items-center gap-2 py-1.5 border-b border-border/50 last:border-b-0",
        isSkip && "opacity-50",
      )}
    >
      <div className="flex-1 min-w-0">
        <div className="font-medium text-sm truncate" title={scraped.name}>
          {scraped.name}
        </div>
        {subtitle && (
          <div className="text-xs text-muted-foreground truncate">
            {subtitle}
          </div>
        )}
      </div>

      {isExisting && (
        <div className="w-56 shrink-0">
          <EntitySingleSelect
            value={value.option}
            onChange={(opt) => {
              if (opt) onChange({ kind: "existing", option: opt });
            }}
            options={searchOptions}
            onSearch={onSearch}
            loading={searching}
            placeholder={intl.formatMessage({
              id: "actions.search",
              defaultMessage: "Search…",
            })}
          />
        </div>
      )}

      {isCreate && (
        <span className="w-56 shrink-0 text-xs text-muted-foreground italic px-2 truncate">
          {labels.willCreate(scraped.name)}
        </span>
      )}

      <div className="flex items-center gap-0.5 shrink-0">
        <Button
          type="button"
          variant={isExisting ? "secondary" : "ghost"}
          size="icon-sm"
          onClick={() => setKind("existing")}
          title={labels.useExisting}
        >
          <Check className="size-3.5" />
        </Button>
        <Button
          type="button"
          variant={isCreate ? "secondary" : "ghost"}
          size="icon-sm"
          onClick={() => setKind("create")}
          title={labels.createNew}
        >
          <PlusCircle className="size-3.5" />
        </Button>
        <Button
          type="button"
          variant={isSkip ? "secondary" : "ghost"}
          size="icon-sm"
          onClick={() => setKind("skip")}
          title={labels.skip}
        >
          <X className="size-3.5" />
        </Button>
      </div>
    </div>
  );
}
