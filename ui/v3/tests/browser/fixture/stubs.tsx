import type { useDefaultFilterActions as appUseDefaultFilterActions } from "@/hooks/default-filter";

// Only backend-dependent integrations are substituted; toolbar, tab, form,
// popover and drawer behavior comes from the production components.
export function useDefaultFilterActions(): ReturnType<
  typeof appUseDefaultFilterActions
> {
  const unexpectedWrite = async () => {
    throw new Error("Default filter writes are outside this fixture's scope");
  };
  return {
    hasDefault: false,
    hasConflict: false,
    saving: false,
    setCurrent: unexpectedWrite,
    clear: unexpectedWrite,
    useLegacy: unexpectedWrite,
    keepV3: unexpectedWrite,
  };
}
