import { createContext } from "react";

/**
 * Exposes the `EntityList` scroll container element to descendants. The
 * virtualizer in `EntityListPage`'s grid mode uses this to observe scroll
 * events on the right element (the inner main column, not window).
 *
 * The element is reactive callback-ref state. The saved offset lets virtual
 * rows mount at the returning viewport instead of first rendering the top.
 */
export const ListScrollContext = createContext<{
  element: HTMLElement | null;
  restorationId: string;
  restorationKey: string;
  initialOffset: number | undefined;
} | null>(null);
