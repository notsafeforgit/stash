import type React from "react";
import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { useLazyQuery } from "@apollo/client/react";
import { useDebouncedValue } from "src/hooks/debounce";
import { useToast } from "src/hooks/toast";
import * as GQL from "src/core/generated-graphql";
import { BadgeCheck } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import { Input } from "src/components/ui/input";
import { Badge } from "src/components/ui/badge";
import { type ScrapeSource, sourceToInput } from "./use-available-scrapers";
import { SearchResultRow, SearchResultsList } from "./search-results";
import { findSceneFingerprintMatches } from "src/utils/fingerprint-matches";

type ScrapedScene = GQL.ScrapedSceneDataFragment;

interface SceneSearchDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  source: ScrapeSource | null;
  initialQuery: string;
  localFiles?: GQL.SceneDataFragment["files"];
  onSelect: (scene: ScrapedScene) => void;
}

export function SceneSearchDialog({
  open,
  onOpenChange,
  source,
  initialQuery,
  localFiles = [],
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
            if (source?.kind === "stashBox") {
              const matches = findSceneFingerprintMatches(
                s.fingerprints ?? [],
                localFiles,
              );
              if (matches.phash.length > 0) {
                const title = matches.phash
                  .map(({ hash, distance }) =>
                    distance === 0
                      ? `${hash}, exact match`
                      : `${hash}, distance ${distance}`,
                  )
                  .join("\n");
                subs.push(
                  <Badge key="phash" variant="outline" title={title}>
                    <BadgeCheck data-icon="inline-start" />
                    {intl.formatMessage(
                      {
                        id: "component_tagger.results.hash_matches",
                        defaultMessage:
                          "{count, plural, one {# {hash_type} match} other {# {hash_type} matches}}",
                      },
                      {
                        count: matches.phash.length,
                        hash_type: "PHash",
                      },
                    )}
                  </Badge>,
                );
              }
              if (matches.oshash.length > 0) {
                const submissionCount = matches.oshash.reduce(
                  (sum, fingerprint) => sum + fingerprint.submissions,
                  0,
                );
                const title = matches.oshash
                  .map(
                    (fingerprint) =>
                      `${fingerprint.hash}, ${intl.formatMessage(
                        {
                          id: "component_tagger.results.hash_submissions",
                          defaultMessage: "{count} submissions",
                        },
                        { count: fingerprint.submissions },
                      )}`,
                  )
                  .join("\n");
                subs.push(
                  <Badge key="oshash" variant="outline" title={title}>
                    <BadgeCheck data-icon="inline-start" />
                    {intl.formatMessage(
                      {
                        id: "component_tagger.results.hash_matches",
                        defaultMessage:
                          "{count, plural, one {# {hash_type} match} other {# {hash_type} matches}}",
                      },
                      {
                        count: submissionCount,
                        hash_type: "OSHash",
                      },
                    )}
                  </Badge>,
                );
              }
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
