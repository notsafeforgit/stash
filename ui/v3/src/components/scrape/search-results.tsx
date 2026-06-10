import type React from "react";
import { useIntl } from "react-intl";
import { cn } from "src/lib/utils";
import { Button } from "src/components/ui/button";
import { Spinner } from "src/components/ui/spinner";

// Two helpers shared by every "search and pick" dialog (scraper search,
// stash-box search, scene picker for reassign). Each dialog still owns its
// own search state + data fetching — only the visual scaffolding lives
// here, so the rendering of a result row stays consistent across flows.

// ── SearchResultRow ─────────────────────────────────────────────────────────

export type SearchResultImageAspect = "square" | "video";

export interface SearchResultRowProps {
  onClick?: () => void;
  /** Visually-distinct selection state (scene-select-dialog uses this for a
   *  two-step "pick → confirm" flow; most callers fire `onClick` directly
   *  and don't supply this). */
  selected?: boolean;
  /** Disabled rows render at half opacity and ignore clicks. Pair with
   *  `title` to explain why. */
  disabled?: boolean;
  /** Hover/disabled tooltip. */
  title?: string;
  imageSrc?: string | null;
  /** "square" → 48px thumbnail (performer/tag/studio).
   *  "video" → 16:9 80×48 thumbnail (scene). */
  imageAspect?: SearchResultImageAspect;
  /** Primary line. */
  primary: React.ReactNode;
  /** Optional stacked subtitle lines (pre-truncated by the caller). */
  subtitles?: React.ReactNode[];
}

export function SearchResultRow({
  onClick,
  selected = false,
  disabled = false,
  title,
  imageSrc,
  imageAspect = "square",
  primary,
  subtitles,
}: SearchResultRowProps) {
  return (
    <Button
      type="button"
      variant="ghost"
      onClick={onClick}
      disabled={disabled}
      title={title}
      data-selected={selected || undefined}
      // Default Button is single-line center-aligned; override to a
      // full-width multi-line list-item layout.
      className={cn(
        "flex h-auto w-full items-start justify-start gap-3 rounded-sm px-1 py-2 text-left whitespace-normal",
        "data-[selected]:bg-secondary",
      )}
    >
      {imageSrc && (
        <img
          src={imageSrc}
          alt=""
          className={cn(
            "shrink-0 rounded-sm bg-muted object-cover",
            imageAspect === "square" ? "size-12" : "h-12 w-20",
          )}
        />
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate font-medium">{primary}</span>
        {subtitles?.map((s, i) => (
          <span key={i} className="truncate text-xs text-muted-foreground">
            {s}
          </span>
        ))}
      </div>
    </Button>
  );
}

// ── SearchResultsList ───────────────────────────────────────────────────────

interface SearchResultsListProps<T> {
  loading: boolean;
  /** True only when the user has typed enough to trigger a search and the
   *  result set is empty. The caller computes this (e.g. checking debounced
   *  query length) so the list can stay quiet for short queries. */
  showNoResults: boolean;
  results: T[];
  renderRow: (item: T, index: number) => React.ReactNode;
  /** Override the default "No matches found" message. */
  noResultsMessage?: string;
}

export function SearchResultsList<T>({
  loading,
  showNoResults,
  results,
  renderRow,
  noResultsMessage,
}: SearchResultsListProps<T>) {
  const intl = useIntl();
  return (
    <div className="-mx-6 max-h-[50vh] overflow-y-auto px-6">
      {loading && (
        <div className="flex items-center justify-center py-6">
          <Spinner />
        </div>
      )}
      {!loading && showNoResults && (
        <p className="py-4 text-center text-sm text-muted-foreground">
          {noResultsMessage ??
            intl.formatMessage({
              id: "scrape.no_results",
              defaultMessage: "No matches found.",
            })}
        </p>
      )}
      {!loading && results.length > 0 && (
        <ul className="flex flex-col divide-y divide-border">
          {results.map((item, i) => (
            <li key={i}>{renderRow(item, i)}</li>
          ))}
        </ul>
      )}
    </div>
  );
}
