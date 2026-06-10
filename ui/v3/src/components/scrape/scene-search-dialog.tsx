import type React from "react";
import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { useLazyQuery } from "@apollo/client/react";
import { useDebouncedValue } from "src/hooks/debounce";
import { useToast } from "src/hooks/toast";
import * as GQL from "src/core/generated-graphql";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import { Input } from "src/components/ui/input";
import { type ScrapeSource, sourceToInput } from "./use-available-scrapers";
import { SearchResultRow, SearchResultsList } from "./search-results";

type ScrapedScene = GQL.ScrapedSceneDataFragment;

interface SceneSearchDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  source: ScrapeSource | null;
  initialQuery: string;
  onSelect: (scene: ScrapedScene) => void;
}

export function SceneSearchDialog({
  open,
  onOpenChange,
  source,
  initialQuery,
  onSelect,
}: SceneSearchDialogProps) {
  const intl = useIntl();
  const toast = useToast();
  const [query, setQuery] = useState(initialQuery);
  const debouncedQuery = useDebouncedValue(query, 300);

  useEffect(() => {
    if (open) setQuery(initialQuery);
  }, [open, initialQuery]);

  const [search, { data, loading, error }] = useLazyQuery(
    GQL.ScrapeSingleSceneDocument,
  );

  useEffect(() => {
    if (!open || !source || debouncedQuery.trim().length < 2) return;
    search({
      variables: {
        source: sourceToInput(source),
        input: { query: debouncedQuery.trim() },
      },
    });
  }, [open, source, debouncedQuery, search]);

  useEffect(() => {
    if (error) toast.error(error);
  }, [error, toast]);

  const results = (data?.scrapeSingleScene ?? []) as ScrapedScene[];
  const noResults =
    !loading &&
    !error &&
    debouncedQuery.trim().length >= 2 &&
    results.length === 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {intl.formatMessage(
              {
                id: "scrape.search_with_scraper",
                defaultMessage: "Search with {scraper}",
              },
              { scraper: source?.name ?? "" },
            )}
          </DialogTitle>
        </DialogHeader>

        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          autoFocus
          placeholder={intl.formatMessage({
            id: "actions.search",
            defaultMessage: "Search…",
          })}
        />

        <SearchResultsList
          loading={loading}
          showNoResults={noResults}
          results={results}
          renderRow={(s) => {
            const subs: React.ReactNode[] = [];
            const meta = [s.studio?.name, s.date].filter(Boolean).join(" · ");
            if (meta) subs.push(meta);
            if (s.performers && s.performers.length > 0) {
              subs.push(
                s.performers
                  .map((p) => p.name)
                  .filter(Boolean)
                  .join(", "),
              );
            }
            return (
              <SearchResultRow
                onClick={() => onSelect(s)}
                imageSrc={s.image}
                imageAspect="video"
                primary={s.title ?? "—"}
                subtitles={subs.length > 0 ? subs : undefined}
              />
            );
          }}
        />
      </DialogContent>
    </Dialog>
  );
}
