import { useCallback, useState } from "react";
import { useIntl } from "react-intl";
import type { View } from "src/components/list/views";
import type {
  ListFilterModel,
  SavedFilterLike,
} from "src/models/list-filter/filter";
import { useConfigurationContextOptional, useConfigureUI } from "./config";
import { useToast } from "./toast";

type ForkDefaultFilterState = {
  filter_ast?: unknown;
  legacy_object_filter?: Record<string, unknown>;
  pending_legacy_object_filter?: Record<string, unknown> | null;
};

type DefaultFilterUIConfig = Record<string, unknown> & {
  defaultFilters?: Partial<
    Record<View, SavedFilterLike & Record<string, unknown>>
  >;
  forkDefaultFilterState?: Partial<Record<View, ForkDefaultFilterState>>;
};

export function useDefaultFilterActions(
  view: View | undefined,
  filter: ListFilterModel,
) {
  const intl = useIntl();
  const Toast = useToast();
  const configuration = useConfigurationContextOptional();
  const [configureUI] = useConfigureUI();
  const [saving, setSaving] = useState(false);
  const ui = configuration?.configuration.ui as
    | DefaultFilterUIConfig
    | undefined;
  const defaultFilter = view ? ui?.defaultFilters?.[view] : undefined;
  const forkState = view ? ui?.forkDefaultFilterState?.[view] : undefined;
  const pendingLegacyFilter = forkState?.pending_legacy_object_filter ?? null;

  const write = useCallback(
    async (
      nextDefault?: SavedFilterLike & Record<string, unknown>,
      nextForkState?: ForkDefaultFilterState,
    ) => {
      if (!view) return;
      const next = structuredClone(ui ?? {}) as DefaultFilterUIConfig;
      const defaults = { ...next.defaultFilters };
      const forkStates = { ...next.forkDefaultFilterState };

      if (nextDefault) defaults[view] = nextDefault;
      else delete defaults[view];
      if (nextForkState) forkStates[view] = nextForkState;
      else delete forkStates[view];

      if (Object.keys(defaults).length > 0) next.defaultFilters = defaults;
      else delete next.defaultFilters;
      if (Object.keys(forkStates).length > 0) {
        next.forkDefaultFilterState = forkStates;
      } else {
        delete next.forkDefaultFilterState;
      }

      await configureUI({ variables: { input: next } });
    },
    [configureUI, ui, view],
  );

  const run = useCallback(
    async (operation: () => Promise<void>, toastID: string) => {
      try {
        setSaving(true);
        await operation();
        Toast.success(intl.formatMessage({ id: toastID }));
      } catch (error) {
        Toast.error(error);
      } finally {
        setSaving(false);
      }
    },
    [intl, Toast],
  );

  const setCurrent = useCallback(() => {
    if (!view) return Promise.resolve();
    const copy = filter.clone();
    const filterAST = copy.makeFilterAst() ?? null;
    const objectFilter = copy.makeLegacyObjectFilter();
    return run(
      () =>
        write(
          {
            mode: filter.mode,
            find_filter: copy.makeFindFilter(),
            object_filter: objectFilter,
            filter_ast: filterAST,
            ui_options: copy.makeSavedUIOptions(),
          },
          {
            filter_ast: filterAST,
            legacy_object_filter: objectFilter,
          },
        ),
      "toast.default_filter_set",
    );
  }, [filter, run, view, write]);

  const clear = useCallback(
    () => run(() => write(), "toast.default_filter_cleared"),
    [run, write],
  );

  const useLegacy = useCallback(() => {
    if (!defaultFilter || !pendingLegacyFilter) return Promise.resolve();
    const imported = filter.empty();
    imported.configureFromSavedFilter({
      ...defaultFilter,
      object_filter: pendingLegacyFilter,
      filter_ast: null,
    });
    const filterAST = imported.makeFilterAst() ?? null;
    return run(
      () =>
        write(
          {
            ...defaultFilter,
            object_filter: pendingLegacyFilter,
            filter_ast: filterAST,
          },
          {
            filter_ast: filterAST,
            legacy_object_filter: pendingLegacyFilter,
          },
        ),
      "toast.default_filter_set",
    );
  }, [defaultFilter, filter, pendingLegacyFilter, run, write]);

  const keepV3 = useCallback(() => {
    if (!defaultFilter || !forkState) return Promise.resolve();
    const legacyObjectFilter = forkState.legacy_object_filter ?? {};
    return run(
      () =>
        write(
          {
            ...defaultFilter,
            object_filter: legacyObjectFilter,
            filter_ast: forkState.filter_ast ?? null,
          },
          {
            filter_ast: forkState.filter_ast ?? null,
            legacy_object_filter: legacyObjectFilter,
          },
        ),
      "toast.default_filter_set",
    );
  }, [defaultFilter, forkState, run, write]);

  return {
    hasDefault: Boolean(defaultFilter),
    hasConflict: Boolean(pendingLegacyFilter),
    saving,
    setCurrent,
    clear,
    useLegacy,
    keepV3,
  };
}
