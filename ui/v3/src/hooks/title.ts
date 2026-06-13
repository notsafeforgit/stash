import { useEffect } from "react";
import { useConfigurationContextOptional } from "src/hooks/config";

export const TITLE = "Stash";
export const TITLE_SEPARATOR = " | ";

/**
 * Sets `document.title` to the given parts followed by the app title
 * (the configured custom UI title, or "Stash"). Falsy parts are dropped,
 * so detail pages can pass an entity name that may not have loaded yet:
 *
 *   useDocumentTitle(scene?.title);            // "My Scene | Stash"
 *   useDocumentTitle(undefined);               // "Stash" until it loads
 *   useDocumentTitle("Scenes");                // "Scenes | Stash"
 *
 * There is intentionally no unmount cleanup: navigating to the next page
 * overwrites the title, and not clearing avoids a flash of the bare app
 * title between an old page unmounting and the new one's effect running.
 */
export function useDocumentTitle(...parts: (string | undefined | null)[]) {
  const config = useConfigurationContextOptional();
  const appTitle = config?.configuration.ui.title || TITLE;

  // Compute here (not in deps) so the effect re-runs whenever any part or
  // the app title changes, without depending on array identity.
  const title = [...parts.filter(Boolean), appTitle].join(TITLE_SEPARATOR);

  useEffect(() => {
    document.title = title;
  }, [title]);
}
