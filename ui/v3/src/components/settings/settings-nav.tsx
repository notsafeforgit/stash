import { useMemo, useState } from "react";
import { useIntl } from "react-intl";
import { Link, useRouterState } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { cn } from "@/lib/utils";
import { Input } from "src/components/ui/input";
import {
  SETTINGS_SEARCH_INDEX,
  type SettingsSearchEntry,
} from "./settings-search-index.gen";

interface NavItem {
  to: string;
  labelId: string;
  defaultLabel: string;
}

const ITEMS: NavItem[] = [
  {
    to: "/settings/tasks",
    labelId: "config.categories.tasks",
    defaultLabel: "Tasks",
  },
  {
    to: "/settings/library",
    labelId: "library",
    defaultLabel: "Library",
  },
  {
    to: "/settings/interface",
    labelId: "config.categories.interface",
    defaultLabel: "Interface",
  },
  {
    to: "/settings/security",
    labelId: "config.categories.security",
    defaultLabel: "Security",
  },
  {
    to: "/settings/metadata-providers",
    labelId: "config.categories.metadata_providers",
    defaultLabel: "Metadata Providers",
  },
  {
    to: "/settings/services",
    labelId: "config.categories.services",
    defaultLabel: "Services",
  },
  {
    to: "/settings/system",
    labelId: "config.categories.system",
    defaultLabel: "System",
  },
  {
    to: "/settings/plugins",
    labelId: "config.categories.plugins",
    defaultLabel: "Plugins",
  },
  {
    to: "/settings/logs",
    labelId: "config.categories.logs",
    defaultLabel: "Logs",
  },
  {
    to: "/settings/tools",
    labelId: "config.categories.tools",
    defaultLabel: "Tools",
  },
  {
    to: "/settings/about",
    labelId: "config.categories.about",
    defaultLabel: "About",
  },
];

const PAGE_LABELS = new Map(ITEMS.map((i) => [i.to, i]));

const MAX_RESULTS = 10;

interface SearchResult {
  entry: SettingsSearchEntry;
  /** Locale-resolved strings, also used for the highlight param. */
  label: string;
  context: string;
}

function isItemActive(item: NavItem, pathname: string) {
  return pathname === item.to || pathname.startsWith(`${item.to}/`);
}

/**
 * Search box over the generated settings index. Matches the resolved
 * label / description / section strings; selecting a result navigates
 * to its page with `?hl=<label>`, which the settings layout uses to
 * scroll to and flash the row.
 */
function SettingsSearch({
  results,
  query,
  setQuery,
}: {
  results: SearchResult[];
  query: string;
  setQuery: (q: string) => void;
}) {
  const intl = useIntl();

  return (
    <div className="space-y-1">
      <div className="relative">
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          type="search"
          className="pl-8"
          placeholder={intl.formatMessage({
            id: "actions.search",
            defaultMessage: "Search…",
          })}
          value={query}
          onChange={(e) => setQuery(e.currentTarget.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") setQuery("");
          }}
        />
      </div>
      {query && (
        <div className="max-h-80 overflow-y-auto rounded-md border bg-card">
          {results.length === 0 ? (
            <p className="px-3 py-2 text-sm text-muted-foreground">
              {intl.formatMessage({
                id: "studio_tagger.no_results_found",
                defaultMessage: "No results found",
              })}
            </p>
          ) : (
            results.map((r) => (
              <Link
                key={`${r.entry.to}|${r.entry.label.id}`}
                to={r.entry.to}
                search={{ hl: r.label }}
                onClick={() => setQuery("")}
                className="block px-3 py-1.5 hover:bg-muted/50"
              >
                <div className="text-sm">{r.label}</div>
                <div className="text-xs text-muted-foreground">{r.context}</div>
              </Link>
            ))
          )}
        </div>
      )}
    </div>
  );
}

export function SettingsNav() {
  const intl = useIntl();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const [query, setQuery] = useState("");

  const results = useMemo<SearchResult[]>(() => {
    const q = query.trim().toLowerCase();
    if (!q) return [];
    const out: SearchResult[] = [];
    for (const entry of SETTINGS_SEARCH_INDEX) {
      const label = intl.formatMessage(entry.label);
      const description = entry.description
        ? intl.formatMessage(entry.description)
        : "";
      const section = entry.section ? intl.formatMessage(entry.section) : "";
      const haystack = `${label} ${description} ${section}`.toLowerCase();
      if (!haystack.includes(q)) continue;
      const page = PAGE_LABELS.get(entry.to);
      const pageLabel = page
        ? intl.formatMessage({
            id: page.labelId,
            defaultMessage: page.defaultLabel,
          })
        : entry.to;
      out.push({
        entry,
        label,
        context: section ? `${pageLabel} › ${section}` : pageLabel,
      });
      if (out.length >= MAX_RESULTS) break;
    }
    return out;
  }, [query, intl]);

  function renderItem(item: NavItem, mobile: boolean) {
    const isActive = isItemActive(item, pathname);
    return (
      <Link
        key={item.to}
        to={item.to}
        className={cn(
          "rounded-md px-3 text-sm font-medium transition-colors",
          mobile ? "whitespace-nowrap py-1.5" : "py-2",
          isActive
            ? "bg-muted text-foreground"
            : "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
        )}
      >
        {intl.formatMessage({
          id: item.labelId,
          defaultMessage: item.defaultLabel,
        })}
      </Link>
    );
  }

  return (
    <>
      {/* Desktop: vertical sidebar */}
      <div className="hidden md:flex md:w-56 md:shrink-0 md:flex-col md:gap-3 md:border-r md:pr-3">
        <SettingsSearch results={results} query={query} setQuery={setQuery} />
        <nav className="flex flex-col gap-1">
          {ITEMS.map((item) => renderItem(item, false))}
        </nav>
      </div>

      {/* Mobile: search above horizontal scrolling tabs */}
      <div className="border-b px-4 py-2 md:hidden">
        <SettingsSearch results={results} query={query} setQuery={setQuery} />
        <nav className="mt-2 flex gap-1 overflow-x-auto">
          {ITEMS.map((item) => renderItem(item, true))}
        </nav>
      </div>
    </>
  );
}
