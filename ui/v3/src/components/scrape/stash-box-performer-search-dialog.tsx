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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import { useAvailablePerformerScrapers } from "./use-available-scrapers";
import { formatGender } from "src/utils/enum-labels";
import { SearchResultRow, SearchResultsList } from "./search-results";

interface StashBoxPerformerSearchDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Endpoints already present in the form's stash-id list — excluded from the
   *  picker so the user can't add a duplicate row. */
  excludeEndpoints: string[];
  /** Prefilled query (typically the performer name from the form). */
  initialQuery: string;
  /** Picked entry — fired after the user picks a result with a remote id. */
  onSelect: (entry: { endpoint: string; stash_id: string }) => void;
}

export function StashBoxPerformerSearchDialog({
  open,
  onOpenChange,
  excludeEndpoints,
  initialQuery,
  onSelect,
}: StashBoxPerformerSearchDialogProps) {
  const intl = useIntl();
  const toast = useToast();
  const { stashBoxes } = useAvailablePerformerScrapers();

  // Stash-boxes the user can actually search — the ones not yet in the form.
  const availableBoxes = useMemo(
    () => stashBoxes.filter((b) => !excludeEndpoints.includes(b.endpoint)),
    [stashBoxes, excludeEndpoints],
  );

  const [endpoint, setEndpoint] = useState<string>("");
  const [query, setQuery] = useState(initialQuery);
  const debouncedQuery = useDebouncedValue(query, 300);

  // Reset state every reopen. Default to the first usable stash-box.
  useEffect(() => {
    if (!open) return;
    setQuery(initialQuery);
    setEndpoint(availableBoxes[0]?.endpoint ?? "");
  }, [open, initialQuery, availableBoxes]);

  const [search, { data, loading, error }] = useLazyQuery(
    GQL.ScrapeSinglePerformerDocument,
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

  const results = (data?.scrapeSinglePerformer ??
    []) as GQL.ScrapedPerformerDataFragment[];
  const noResults =
    !loading &&
    !error &&
    debouncedQuery.trim().length >= 2 &&
    results.length === 0;

  function handlePick(p: GQL.ScrapedPerformerDataFragment) {
    if (!endpoint || !p.remote_site_id) return;
    onSelect({ endpoint, stash_id: p.remote_site_id });
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
                id: "scrape.stash_box_search_placeholder",
                defaultMessage: "Performer name or stash-id (UUID)",
              })}
            />

            <SearchResultsList
              loading={loading}
              showNoResults={noResults}
              results={results}
              renderRow={(p) => {
                const meta = [
                  formatGender(intl, p.gender),
                  p.birthdate,
                  p.country,
                ]
                  .filter(Boolean)
                  .join(" · ");
                return (
                  <SearchResultRow
                    onClick={() => handlePick(p)}
                    disabled={!p.remote_site_id}
                    title={
                      !p.remote_site_id
                        ? intl.formatMessage({
                            id: "scrape.no_remote_id",
                            defaultMessage:
                              "This result has no remote stash-id.",
                          })
                        : undefined
                    }
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
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
