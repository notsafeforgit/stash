import { useCallback, useMemo, useState, type SetStateAction } from "react";
import { DuplicateFilterMode, type FilterMode } from "@/core/generated-graphql";
import { ListFilterModel } from "@/models/list-filter/filter";
import type { DuplicateFilterScope } from "@/components/filters/duplicate-filter-scope-toggle";

export interface DuplicateFilterSearch {
  fa?: string;
  page?: number;
}

/** Committed filters belong to route search. The sidebar clones this model for
 * edits; history changes and same-route navigation require no synchronization. */
export function useDuplicateFilter(
  mode: FilterMode,
  fa: string | undefined,
  write: (search: DuplicateFilterSearch) => void,
) {
  const filterModel = useMemo(() => {
    const model = new ListFilterModel(mode);
    if (fa) model.configureFromDecodedParams({ fa });
    return model;
  }, [mode, fa]);
  return {
    filterModel,
    setFilter: (next: ListFilterModel) =>
      write({ fa: next.getEncodedParams().fa ?? undefined, page: undefined }),
  };
}

type Checked = Record<string, boolean>;
/** Selection cannot follow a different query/page into a destructive action. */
export function useDuplicateSelection(scope: string) {
  const [snapshot, setSnapshot] = useState<{ scope: string; checked: Checked }>(
    { scope, checked: {} },
  );
  const current = snapshot.scope === scope ? snapshot : { scope, checked: {} };
  if (current !== snapshot) setSnapshot(current);
  const setChecked = useCallback(
    (update: SetStateAction<Checked>) => {
      setSnapshot((previous) => ({
        scope,
        checked:
          typeof update === "function"
            ? update(previous.scope === scope ? previous.checked : {})
            : update,
      }));
    },
    [scope],
  );
  return [current.checked, setChecked] as const;
}

export function pageCount(total: number, size: number): number {
  return Math.max(1, Math.ceil(total / size));
}
export function duplicateFilterMode(
  scope: DuplicateFilterScope,
): DuplicateFilterMode {
  return scope === "any" ? DuplicateFilterMode.Any : DuplicateFilterMode.All;
}
