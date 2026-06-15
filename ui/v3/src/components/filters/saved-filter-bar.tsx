import type React from "react";
import { useCallback, useMemo, useState, useEffect } from "react";
import { FormattedMessage, useIntl } from "react-intl";
import { Button } from "src/components/ui/button";
import {
  PinButton,
  PinnableComboBox,
} from "src/components/ui/pinnable-combo-box";
import type { SavedFilterDataFragment } from "src/core/generated-graphql";
import type { ListFilterModel } from "src/models/list-filter/filter";
import { SaveFilterDialog } from "./saved-filter-list";
import { useFindSavedFilters, useSaveFilter } from "src/core/saved-filters";
import { useToast } from "src/hooks/toast";
import { getPinnedSavedFiltersKey } from "./filter-builder-types";
import type { View } from "src/components/list/views";
import { useConfigureUISetting } from "src/hooks/config";

function readPinnedSavedFilters(mode: string): string[] {
  if (typeof window === "undefined") return [];
  try {
    const raw = window.localStorage.getItem(
      getPinnedSavedFiltersKey(mode as never),
    );
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed)
      ? parsed.filter((v): v is string => typeof v === "string")
      : [];
  } catch {
    return [];
  }
}

function usePinnedSavedFilters(mode: string) {
  const [pinnedIds, setPinnedIds] = useState<string[]>(() =>
    readPinnedSavedFilters(mode),
  );

  useEffect(() => {
    if (typeof window === "undefined") return;
    window.localStorage.setItem(
      getPinnedSavedFiltersKey(mode as never),
      JSON.stringify(pinnedIds),
    );
  }, [mode, pinnedIds]);

  const togglePinned = useCallback((id: string) => {
    setPinnedIds((current) =>
      current.includes(id) ? current.filter((v) => v !== id) : [...current, id],
    );
  }, []);

  return { pinnedIds, togglePinned };
}

export const SavedFilterBar: React.FC<{
  filter: ListFilterModel;
  setFilter: (filter: ListFilterModel) => void;
  currentSavedFilterName?: string;
  onCurrentSavedFilterChange: (next?: {
    id?: string;
    name: string;
    justApplied?: boolean;
  }) => void;
  /** When provided, shows controls to set/clear the default filter for this view. */
  view?: View;
}> = ({
  filter,
  setFilter,
  currentSavedFilterName,
  onCurrentSavedFilterChange,
  view,
}) => {
  const intl = useIntl();
  const Toast = useToast();
  const { data, previousData, refetch } = useFindSavedFilters(filter.mode);
  const saveFilter = useSaveFilter();
  const [showSaveDialog, setShowSaveDialog] = useState(false);
  const [saving, setSaving] = useState(false);
  const [settingDefault, setSettingDefault] = useState(false);
  const { pinnedIds, togglePinned } = usePinnedSavedFilters(filter.mode);
  const [saveUISetting] = useConfigureUISetting();

  const savedFilters = useMemo(
    () => data?.findSavedFilters ?? previousData?.findSavedFilters ?? [],
    [data, previousData],
  );

  const savedFilterOptions = useMemo(
    () => savedFilters.map((f) => ({ value: f.id, label: f.name })),
    [savedFilters],
  );

  const selectedFilterId =
    savedFilters.find((f) => f.name === currentSavedFilterName)?.id ?? "";

  const loadSavedFilter = useCallback(
    (savedFilter: SavedFilterDataFragment) => {
      const newFilter = filter.clone();
      newFilter.currentPage = 1;
      newFilter.searchTerm = "";
      newFilter.configureFromSavedFilter(savedFilter);
      newFilter.randomSeed = -1;
      setFilter(newFilter);
      onCurrentSavedFilterChange({
        id: savedFilter.id,
        name: savedFilter.name,
        justApplied: true,
      });
    },
    [filter, onCurrentSavedFilterChange, setFilter],
  );

  const onSaveCurrentFilter = useCallback(
    async (name: string, id?: string) => {
      try {
        setSaving(true);
        const result = await saveFilter(filter.clone(), name, id);
        const saved = result.data?.saveFilter;
        onCurrentSavedFilterChange({ id: saved?.id ?? id, name });
        setShowSaveDialog(false);
        Toast.success(
          intl.formatMessage(
            { id: "toast.saved_entity" },
            {
              entity: intl.formatMessage({ id: "filter" }).toLocaleLowerCase(),
            },
          ),
        );
        refetch();
      } catch (error) {
        Toast.error(error);
      } finally {
        setSaving(false);
      }
    },
    [filter, intl, onCurrentSavedFilterChange, refetch, saveFilter, Toast],
  );

  const onSetDefaultFilter = useCallback(async () => {
    if (!view) return;
    const filterCopy = filter.clone();
    try {
      setSettingDefault(true);
      await saveUISetting({
        variables: {
          key: `defaultFilters.${view}`,
          value: {
            mode: filter.mode,
            find_filter: filterCopy.makeFindFilter(),
            filter_ast: filterCopy.makeFilterAst(),
            ui_options: filterCopy.makeSavedUIOptions(),
          },
        },
      });
      Toast.success(intl.formatMessage({ id: "toast.default_filter_set" }));
    } catch (error) {
      Toast.error(error);
    } finally {
      setSettingDefault(false);
    }
  }, [view, filter, saveUISetting, intl, Toast]);

  const triggerLabel =
    currentSavedFilterName ??
    intl.formatMessage({
      id: "search_filter.saved_filters",
      defaultMessage: "Saved filters",
    });

  return (
    <>
      {showSaveDialog && (
        <SaveFilterDialog
          mode={filter.mode}
          onClose={(name, id) => {
            if (!name) {
              setShowSaveDialog(false);
              return;
            }
            onSaveCurrentFilter(name, id);
          }}
          isSaving={saving}
        />
      )}
      <div className="rounded-lg border bg-card p-3 flex flex-col gap-2">
        <div className="flex items-center gap-2 flex-wrap">
          <PinnableComboBox
            currentLabel={triggerLabel}
            options={savedFilterOptions}
            selectedValue={selectedFilterId}
            searchPlaceholder="Search saved filters…"
            pinnedValues={pinnedIds}
            pinnedSectionLabel="Pinned"
            allSectionLabel="Saved filters"
            onSelect={(id) => {
              const sf = savedFilters.find((f) => f.id === id);
              if (sf) loadSavedFilter(sf);
            }}
            renderItemAddon={(id, isPinned) => (
              <PinButton pinned={isPinned} onToggle={() => togglePinned(id)} />
            )}
          />
          <Button variant="outline" onClick={() => setShowSaveDialog(true)}>
            <FormattedMessage
              id="actions.save_filter"
              defaultMessage="Save current filter"
            />
          </Button>
        </div>
        {view && (
          <div className="flex items-center gap-2 flex-wrap border-t border-border pt-2">
            <Button
              variant="outline"
              size="sm"
              disabled={settingDefault}
              onClick={onSetDefaultFilter}
            >
              <FormattedMessage id="actions.set_current_filter_as_default" />
            </Button>
          </div>
        )}
      </div>
    </>
  );
};
