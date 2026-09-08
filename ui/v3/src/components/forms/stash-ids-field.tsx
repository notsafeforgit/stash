import { useEditableRows } from "@/hooks/use-editable-rows";
import { useState } from "react";
import { PlusIcon, SearchIcon, Trash2Icon } from "lucide-react";
import { useIntl } from "react-intl";
import { Button } from "src/components/ui/button";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "src/components/ui/input-group";
import { useAvailableStashBoxes } from "src/components/scrape/use-available-scrapers";
import { StashBoxPerformerSearchDialog } from "src/components/scrape/stash-box-performer-search-dialog";
import { StashBoxSceneSearchDialog } from "src/components/scrape/stash-box-scene-search-dialog";
import { StashBoxStudioSearchDialog } from "src/components/scrape/stash-box-studio-search-dialog";
import { StashBoxTagSearchDialog } from "src/components/scrape/stash-box-tag-search-dialog";

export interface StashIdEntry {
  endpoint: string;
  stash_id: string;
}

interface StashIdsFieldProps {
  value: StashIdEntry[];
  onChange: (ids: StashIdEntry[]) => void;
  disabled?: boolean;
  /** When set, exposes a "Search stash-box" button that opens a search dialog
   *  for the given entity type, populating the row with the picked result's
   *  remote_site_id. */
  searchType?: "performer" | "scene" | "studio" | "tag";
  /** Prefilled query for the search dialog (e.g. the entity's name). */
  searchQuery?: string;
}

export function StashIdsField({
  value,
  onChange,
  disabled = false,
  searchType,
  searchQuery = "",
}: StashIdsFieldProps) {
  const intl = useIntl();

  // The stash-id picker only needs the configured stash-boxes. We avoid
  // pulling the full per-type scraper lists here — those are only needed by
  // the "Scrape with…" menu that lives in the entity edit forms.
  const { stashBoxes } = useAvailableStashBoxes();
  const showSearchButton = !!searchType && stashBoxes.length > 0;
  const [searchOpen, setSearchOpen] = useState(false);

  const { rows, update, remove, append } = useEditableRows(value, onChange);

  function updateField(index: number, field: keyof StashIdEntry, text: string) {
    const row = rows[index];
    if (row) update(index, { ...row.value, [field]: text });
  }

  function upsertSearchResult(entry: StashIdEntry) {
    const index = rows.findIndex(
      (row) => row.value.endpoint === entry.endpoint,
    );
    if (index >= 0) update(index, entry);
    else append(entry);
  }

  return (
    <div className="flex flex-col gap-1.5">
      {rows.map(({ key, value: entry }, i) => (
        <div key={key} className="flex gap-1.5">
          <InputGroup className="flex-1">
            <InputGroupInput
              value={entry.endpoint}
              placeholder="Endpoint URL"
              disabled={disabled}
              onChange={(e) => updateField(i, "endpoint", e.target.value)}
            />
          </InputGroup>
          <InputGroup className="flex-1">
            <InputGroupInput
              value={entry.stash_id}
              placeholder="Stash ID"
              disabled={disabled}
              onChange={(e) => updateField(i, "stash_id", e.target.value)}
            />
            <InputGroupAddon align="inline-end" className="pr-0">
              <InputGroupButton
                size="icon-xs"
                variant="ghost"
                disabled={disabled}
                aria-label="Remove stash ID"
                onClick={() => remove(i)}
              >
                <Trash2Icon className="pointer-events-none size-3.5" />
              </InputGroupButton>
            </InputGroupAddon>
          </InputGroup>
        </div>
      ))}
      <div className="flex flex-wrap gap-1.5">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled}
          onClick={() => append({ endpoint: "", stash_id: "" })}
        >
          <PlusIcon className="size-3.5" />
          Add Stash ID
        </Button>
        {showSearchButton && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={disabled}
            onClick={() => setSearchOpen(true)}
          >
            <SearchIcon className="size-3.5" />
            {intl.formatMessage({
              id: "scrape.stash_box_search",
              defaultMessage: "Search stash-box",
            })}
          </Button>
        )}
      </div>
      {searchType === "performer" && (
        <StashBoxPerformerSearchDialog
          open={searchOpen}
          onOpenChange={setSearchOpen}
          excludeEndpoints={value.map((e) => e.endpoint).filter(Boolean)}
          initialQuery={searchQuery}
          onSelect={upsertSearchResult}
        />
      )}
      {searchType === "scene" && (
        <StashBoxSceneSearchDialog
          open={searchOpen}
          onOpenChange={setSearchOpen}
          excludeEndpoints={value.map((e) => e.endpoint).filter(Boolean)}
          initialQuery={searchQuery}
          onSelect={upsertSearchResult}
        />
      )}
      {searchType === "studio" && (
        <StashBoxStudioSearchDialog
          open={searchOpen}
          onOpenChange={setSearchOpen}
          excludeEndpoints={value.map((e) => e.endpoint).filter(Boolean)}
          initialQuery={searchQuery}
          onSelect={upsertSearchResult}
        />
      )}
      {searchType === "tag" && (
        <StashBoxTagSearchDialog
          open={searchOpen}
          onOpenChange={setSearchOpen}
          excludeEndpoints={value.map((e) => e.endpoint).filter(Boolean)}
          initialQuery={searchQuery}
          onSelect={upsertSearchResult}
        />
      )}
    </div>
  );
}
