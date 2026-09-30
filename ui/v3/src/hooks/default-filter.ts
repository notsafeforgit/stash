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
  defaultFilterConflicts?: Partial<
    Record<View, { revision: number; import_error?: string }>
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
  const conflict = view ? ui?.defaultFilterConflicts?.[view] : undefined;

  const write = useCallback(
    async (action: DefaultFilterAction, nextFilter?: DefaultFilterInput) => {
      if (!view) return;
      try {
        await configureDefaultFilter({
          variables: {
            input: {
              view,
              action,
              filter: nextFilter,
              expected_revision: conflict?.revision,
            },
          },
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
    [configureDefaultFilter, intl, Toast, view, conflict?.revision],
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
  const useImported = useCallback(
    () => write(DefaultFilterAction.UseImported),
    [write],
  );
  const keepCurrent = useCallback(
    () => write(DefaultFilterAction.KeepCurrent),
    [write],
  );

  return {
    hasDefault: Boolean(defaultFilter),
    hasConflict: Boolean(conflict),
    canUseImported: !conflict?.import_error,
    saving,
    setCurrent,
    clear,
    useImported,
    keepCurrent,
  };
}
