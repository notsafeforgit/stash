import { useMemo, useState, type Ref } from "react";
import { useIntl } from "react-intl";
import { Link } from "@tanstack/react-router";
import { Search } from "lucide-react";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group";
import { cn } from "@/lib/utils";
import { SETTINGS_SEARCH_INDEX } from "./settings-search-index.gen";
import {
  SETTINGS_NAV_ITEMS,
  type SettingsNavItem,
} from "./settings-navigation";

const PAGE_LABELS = new Map<string, SettingsNavItem>(
  SETTINGS_NAV_ITEMS.map((item) => [item.to, item]),
);
const MAX_RESULTS = 10;

interface SearchResult {
  key: string;
  to: SettingsNavItem["to"];
  /** Locale-resolved label, also used by the destination's highlight param. */
  label: string;
  context: string;
}

export function useSettingsSearch() {
  const intl = useIntl();
  const [query, setQuery] = useState("");
  const results = useMemo<SearchResult[]>(() => {
    const q = query.trim().toLowerCase();
    if (!q) return [];
    const out: SearchResult[] = [];
    for (const entry of SETTINGS_SEARCH_INDEX) {
      const page = PAGE_LABELS.get(entry.to);
      if (!page) continue;
      const label = intl.formatMessage(entry.label);
      const description = entry.description
        ? intl.formatMessage(entry.description)
        : "";
      const section = entry.section ? intl.formatMessage(entry.section) : "";
      if (!`${label} ${description} ${section}`.toLowerCase().includes(q))
        continue;
      const pageLabel = intl.formatMessage({
        id: page.labelId,
        defaultMessage: page.defaultLabel,
      });
      out.push({
        key: `${entry.to}|${entry.label.id}`,
        to: page.to,
        label,
        context: section ? `${pageLabel} › ${section}` : pageLabel,
      });
      if (out.length >= MAX_RESULTS) break;
    }
    return out;
  }, [query, intl]);
  return { query, setQuery, results };
}

export function SettingsSearchInput({
  query,
  setQuery,
  inputRef,
  mobile = false,
  onClose,
}: Pick<ReturnType<typeof useSettingsSearch>, "query" | "setQuery"> & {
  inputRef?: Ref<HTMLInputElement>;
  mobile?: boolean;
  onClose?: () => void;
}) {
  const intl = useIntl();
  return (
    <InputGroup className={cn(mobile && "h-11 flex-1")}>
      <InputGroupInput
        ref={inputRef}
        type="search"
        aria-label={intl.formatMessage({ id: "accessibility.search_settings" })}
        placeholder={intl.formatMessage({ id: "search" })}
        className={cn(mobile && "h-full")}
        value={query}
        onChange={(event) => setQuery(event.currentTarget.value)}
        onKeyDown={(event) => {
          if (event.key !== "Escape") return;
          // The field remains mounted during its exit animation. Prevent the
          // browser's native search-input clear action from erasing the query.
          event.preventDefault();
          if (onClose) onClose();
          else setQuery("");
        }}
      />
      <InputGroupAddon>
        <Search />
      </InputGroupAddon>
    </InputGroup>
  );
}

/** Search destinations remain ordinary links in both navigation layouts. */
export function SettingsSearchResults({
  results,
  onSelect,
  mobile = false,
}: {
  results: readonly SearchResult[];
  onSelect: () => void;
  mobile?: boolean;
}) {
  const intl = useIntl();
  return results.length === 0 ? (
    <p role="status" className="px-3 py-2 text-sm text-muted-foreground">
      {intl.formatMessage({ id: "studio_tagger.no_results_found" })}
    </p>
  ) : (
    <nav
      aria-label={intl.formatMessage({
        id: "accessibility.settings_search_results",
      })}
    >
      {results.map((result) => (
        <Link
          key={result.key}
          to={result.to}
          search={{ hl: result.label }}
          onClick={onSelect}
          className={cn(
            "flex flex-col justify-center px-3 py-1.5 hover:bg-muted/50 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring",
            mobile && "min-h-11 py-2",
          )}
        >
          <span className="text-sm">{result.label}</span>
          <span className="text-xs text-muted-foreground">
            {result.context}
          </span>
        </Link>
      ))}
    </nav>
  );
}
