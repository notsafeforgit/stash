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
import { formatGender } from "src/utils/enum-labels";
import { SearchResultRow, SearchResultsList } from "./search-results";

type ScrapedPerformer = GQL.ScrapedPerformerDataFragment;

interface PerformerSearchDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  source: ScrapeSource | null;
  initialQuery: string;
  onSelect: (performer: ScrapedPerformer) => void;
}

export function PerformerSearchDialog({
  open,
  onOpenChange,
  source,
  initialQuery,
  onSelect,
}: PerformerSearchDialogProps) {
  const intl = useIntl();
  const toast = useToast();
  const [query, setQuery] = useState(initialQuery);
  const debouncedQuery = useDebouncedValue(query, 300);

  // Reset the editable query each time the dialog reopens with a new scraper /
  // initial seed. Without this the previous search term persists across open
  // cycles, which is wrong when the user moves between performers.
  useEffect(() => {
    if (open) setQuery(initialQuery);
  }, [open, initialQuery]);

  const [search, { data, loading, error }] = useLazyQuery(
    GQL.ScrapeSinglePerformerDocument,
  );

  // Empty / very short queries are skipped — most scrapers reject them anyway
  // and we don't want to spam the network on each keystroke.
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

  const results = (data?.scrapeSinglePerformer ?? []) as ScrapedPerformer[];
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
          renderRow={(p) => {
            const meta = [formatGender(intl, p.gender), p.birthdate, p.country]
              .filter(Boolean)
              .join(" · ");
            return (
              <SearchResultRow
                onClick={() => onSelect(p)}
                imageSrc={p.images?.[0]}
                primary={
                  <>
                    {p.name ?? "—"}
                    {p.disambiguation && (
                      <span className="ml-1.5 font-normal text-muted-foreground">
                        ({p.disambiguation})
                      </span>
                    )}
                  </>
                }
                subtitles={meta ? [meta] : undefined}
              />
            );
          }}
        />
      </DialogContent>
    </Dialog>
  );
}
