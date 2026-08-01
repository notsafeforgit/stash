import { createContext } from "react";

/**
 * Exposes the `EntityList` scroll container element to descendants. The
 * virtualizer in `EntityListPage`'s grid mode uses this to observe scroll
 * events on the right element (the inner main column, not window).
 *
 * The value is the DOM element directly (not a ref) so consumers can react
 * to it in render — `useContext` will return `null` on the first commit and
 * the actual element on the second, which is exactly when the virtualizer
 * needs to start computing rows.
 */
export const ListScrollContext = createContext<HTMLElement | null>(null);
