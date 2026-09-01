import { createContext, useContext } from "react";

/**
 * Whether a list is currently visible and allowed to own page-level
 * interactions. Standalone lists are active by default; detail tabs provide
 * an explicit value so their keep-mounted, hidden panels stay passive.
 */
export const ListActivityContext = createContext(true);

export function useListActivity() {
  return useContext(ListActivityContext);
}
