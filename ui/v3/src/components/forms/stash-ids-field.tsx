import { useRef, useState } from "react";
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

let stashIdRowIdCounter = 0;
const makeRowId = () => `stash-id-row-${++stashIdRowIdCounter}`;

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

  // Stable per-row keys — see url-list-field.tsx for the rationale.
  // `key={i}` lets React mismatch row DOM nodes during mid-edit
  // re-renders, which surfaces on mobile as taps focusing the wrong
  // input. Internal mutations keep keysRef and `value` in lockstep;
  // external value changes (form reset, scrape result) trigger
  // regeneration via the length-mismatch branch.
  const keysRef = useRef<string[]>([]);
  if (keysRef.current.length !== value.length) {
    keysRef.current = value.map((_, i) => keysRef.current[i] ?? makeRowId());
  }

  function updateField(index: number, field: keyof StashIdEntry, text: string) {
    const next = [...value];
    next[index] = { ...next[index], [field]: text };
    onChange(next);
  }

  function remove(index: number) {
    keysRef.current = keysRef.current.filter((_, i) => i !== index);
    onChange(value.filter((_, i) => i !== index));
  }

  function add() {
    keysRef.current = [...keysRef.current, makeRowId()];
    onChange([...value, { endpoint: "", stash_id: "" }]);
  }

  function upsertSearchResult(entry: StashIdEntry) {
    const idx = value.findIndex((e) => e.endpoint === entry.endpoint);
    if (idx >= 0) {
      const next = [...value];
      next[idx] = entry;
      onChange(next);
      return;
    }
    keysRef.current = [...keysRef.current, makeRowId()];
    onChange([...value, entry]);
  }

  return (
    <div className="flex flex-col gap-1.5">
      {value.map((entry, i) => (
        <div key={keysRef.current[i]} className="flex gap-1.5">
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
          onClick={add}
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
