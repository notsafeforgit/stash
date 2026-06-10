import { useEffect, useMemo, useState } from "react";
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
import { SearchResultRow, SearchResultsList } from "./search-results";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import { useAvailableStudioScrapers } from "./use-available-scrapers";

interface StashBoxStudioSearchDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  excludeEndpoints: string[];
  initialQuery: string;
  onSelect: (entry: { endpoint: string; stash_id: string }) => void;
}

export function StashBoxStudioSearchDialog({
  open,
  onOpenChange,
  excludeEndpoints,
  initialQuery,
  onSelect,
}: StashBoxStudioSearchDialogProps) {
  const intl = useIntl();
  const toast = useToast();
  const { stashBoxes } = useAvailableStudioScrapers();

  const availableBoxes = useMemo(
    () => stashBoxes.filter((b) => !excludeEndpoints.includes(b.endpoint)),
    [stashBoxes, excludeEndpoints],
  );

  const [endpoint, setEndpoint] = useState<string>("");
  const [query, setQuery] = useState(initialQuery);
  const debouncedQuery = useDebouncedValue(query, 300);

  useEffect(() => {
    if (!open) return;
    setQuery(initialQuery);
    setEndpoint(availableBoxes[0]?.endpoint ?? "");
  }, [open, initialQuery, availableBoxes]);

  const [search, { data, loading, error }] = useLazyQuery(
    GQL.ScrapeSingleStudioDocument,
  );

  useEffect(() => {
    if (!open || !endpoint || debouncedQuery.trim().length < 2) return;
    search({
      variables: {
        source: { stash_box_endpoint: endpoint },
        input: { query: debouncedQuery.trim() },
      },
    });
  }, [open, endpoint, debouncedQuery, search]);

  useEffect(() => {
    if (error) toast.error(error);
  }, [error, toast]);

  // scrapeSingleStudio returns a single ScrapedStudio (not an array). Wrap so
  // the UI can present "no result" and "1 result" with the same code path.
  const single = data?.scrapeSingleStudio as
    | GQL.ScrapedStudioDataFragment
    | null
    | undefined;
  const results = single ? [single] : [];
  const noResults =
    !loading && !error && debouncedQuery.trim().length >= 2 && !single;

  function handlePick(s: GQL.ScrapedStudioDataFragment) {
    if (!endpoint || !s.remote_site_id) return;
    onSelect({ endpoint, stash_id: s.remote_site_id });
    onOpenChange(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {intl.formatMessage({
              id: "scrape.stash_box_search",
              defaultMessage: "Search stash-box",
            })}
          </DialogTitle>
        </DialogHeader>

        {availableBoxes.length === 0 ? (
          <p className="text-sm text-muted-foreground py-4 text-center">
            {intl.formatMessage({
              id: "scrape.stash_box_none_available",
              defaultMessage:
                "Every configured stash-box is already in this list.",
            })}
          </p>
        ) : (
          <>
            <Select
              value={endpoint}
              onValueChange={(v) => setEndpoint(v ?? "")}
            >
              <SelectTrigger className="w-full">
                <SelectValue>
                  {availableBoxes.find((b) => b.endpoint === endpoint)?.name ??
                    ""}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {availableBoxes.map((b) => (
                  <SelectItem key={b.endpoint} value={b.endpoint}>
                    {b.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              autoFocus
              placeholder={intl.formatMessage({
                id: "scrape.stash_box_search_placeholder_studio",
                defaultMessage: "Studio name or stash-id",
              })}
            />

            <SearchResultsList
              loading={loading}
              showNoResults={noResults}
              results={results}
              renderRow={(s) => (
                <SearchResultRow
                  onClick={() => handlePick(s)}
                  disabled={!s.remote_site_id}
                  title={
                    !s.remote_site_id
                      ? intl.formatMessage({
                          id: "scrape.no_remote_id",
                          defaultMessage: "This result has no remote stash-id.",
                        })
                      : undefined
                  }
                  imageSrc={s.image}
                  primary={s.name}
                  subtitles={s.urls?.[0] ? [s.urls[0]] : undefined}
                />
              )}
            />
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
