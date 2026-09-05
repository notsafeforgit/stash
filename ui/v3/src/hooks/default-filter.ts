import { useCallback } from "react";
import { useIntl } from "react-intl";
import type { View } from "src/components/list/views";
import type {
  ListFilterModel,
  SavedFilterLike,
} from "src/models/list-filter/filter";
import {
  DefaultFilterAction,
  type DefaultFilterInput,
} from "src/core/generated-graphql";
import {
  useConfigurationContextOptional,
  useConfigureDefaultFilter,
} from "./config";
import { useToast } from "./toast";

type DefaultFilterUIConfig = {
  defaultFilters?: Partial<Record<View, SavedFilterLike>>;
  forkDefaultFilterState?: Partial<
    Record<View, { pending_legacy_object_filter?: unknown }>
  >;
};

export function useDefaultFilterActions(
  view: View | undefined,
  filter: ListFilterModel,
) {
  const intl = useIntl();
  const Toast = useToast();
  const configuration = useConfigurationContextOptional();
  const [configureDefaultFilter, { loading: saving }] =
    useConfigureDefaultFilter();
  const ui = configuration?.configuration.ui as
    | DefaultFilterUIConfig
    | undefined;
  const defaultFilter = view ? ui?.defaultFilters?.[view] : undefined;
  const forkState = view ? ui?.forkDefaultFilterState?.[view] : undefined;

  const write = useCallback(
    async (action: DefaultFilterAction, nextFilter?: DefaultFilterInput) => {
      if (!view) return;
      try {
        await configureDefaultFilter({
          variables: { input: { view, action, filter: nextFilter } },
        });
        Toast.success(
          intl.formatMessage({
            id:
              action === DefaultFilterAction.Clear
                ? "toast.default_filter_cleared"
                : "toast.default_filter_set",
          }),
        );
      } catch {
        // The tracked save reports the error and updates the save indicator.
      }
    },
    [configureDefaultFilter, intl, Toast, view],
  );

  const setCurrent = useCallback(() => {
    const copy = filter.clone();
    return write(DefaultFilterAction.Set, {
      mode: copy.mode,
      find_filter: copy.makeFindFilter(),
      filter_ast: copy.makeFilterAst() ?? null,
      ui_options: copy.makeSavedUIOptions(),
    });
  }, [filter, write]);
  const clear = useCallback(() => write(DefaultFilterAction.Clear), [write]);
  const useLegacy = useCallback(
    () => write(DefaultFilterAction.UseLegacy),
    [write],
  );
  const keepV3 = useCallback(() => write(DefaultFilterAction.KeepV3), [write]);

  return {
    hasDefault: Boolean(defaultFilter),
    hasConflict: Boolean(
      forkState && "pending_legacy_object_filter" in forkState,
    ),
    saving,
    setCurrent,
    clear,
    useLegacy,
    keepV3,
  };
}
