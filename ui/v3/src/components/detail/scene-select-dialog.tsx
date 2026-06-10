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
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import { Input } from "src/components/ui/input";
import { Button } from "src/components/ui/button";
import {
  SearchResultRow,
  SearchResultsList,
} from "src/components/scrape/search-results";

export interface SceneSummary {
  id: string;
  title: string;
  date?: string | null;
  studioName?: string | null;
  filePath?: string | null;
  screenshot?: string | null;
}

interface SceneSelectDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Title shown in the header. */
  title: string;
  /** Confirm-button label. Defaults to "Select". */
  confirmLabel?: string;
  /** Scene IDs to filter out of the result list (e.g. the current scene). */
  excludeIds?: string[];
  /** Initial query to seed the search input. */
  initialQuery?: string;
  onSelect: (scene: SceneSummary) => void;
}

export function SceneSelectDialog({
  open,
  onOpenChange,
  title,
  confirmLabel,
  excludeIds = [],
  initialQuery = "",
  onSelect,
}: SceneSelectDialogProps) {
  const intl = useIntl();
  const toast = useToast();
  const [query, setQuery] = useState(initialQuery);
  const debouncedQuery = useDebouncedValue(query, 250);
  const [picked, setPicked] = useState<SceneSummary | null>(null);

  useEffect(() => {
    if (open) {
      setQuery(initialQuery);
      setPicked(null);
    }
  }, [open, initialQuery]);

  const [search, { data, loading, error }] = useLazyQuery(
    GQL.FindScenesDocument,
  );

  useEffect(() => {
    if (!open) return;
    const q = debouncedQuery.trim();
    if (q.length < 2) return;
    search({ variables: { filter: { q, per_page: 25 } } });
  }, [open, debouncedQuery, search]);

  useEffect(() => {
    if (error) toast.error(error);
  }, [error, toast]);

  const excluded = new Set(excludeIds);
  const results: SceneSummary[] = (data?.findScenes.scenes ?? [])
    .filter((s) => !excluded.has(s.id))
    .map((s) => ({
      id: s.id,
      title: s.title?.trim() || s.files[0]?.path || `Scene ${s.id}`,
      date: s.date,
      studioName: s.studio?.name,
      filePath: s.files[0]?.path,
      screenshot: s.paths.screenshot,
    }));
  const noResults =
    !loading &&
    !error &&
    debouncedQuery.trim().length >= 2 &&
    results.length === 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
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
            const meta = [s.studioName, s.date].filter(Boolean).join(" · ");
            if (meta) subs.push(meta);
            if (s.filePath) {
              subs.push(<span className="font-mono">{s.filePath}</span>);
            }
            return (
              <SearchResultRow
                onClick={() => setPicked(s)}
                selected={picked?.id === s.id}
                imageSrc={s.screenshot}
                imageAspect="video"
                primary={s.title}
                subtitles={subs.length > 0 ? subs : undefined}
              />
            );
          }}
        />

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            {intl.formatMessage({
              id: "actions.cancel",
              defaultMessage: "Cancel",
            })}
          </Button>
          <Button
            type="button"
            disabled={!picked}
            onClick={() => {
              if (picked) {
                onSelect(picked);
                onOpenChange(false);
              }
            }}
          >
            {confirmLabel ??
              intl.formatMessage({
                id: "actions.select",
                defaultMessage: "Select",
              })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
