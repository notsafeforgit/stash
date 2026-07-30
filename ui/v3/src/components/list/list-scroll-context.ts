import { createContext, useContext } from "react";

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

export type FinishListDeletionScrollPreservation = (succeeded: boolean) => void;
export type BeginListDeletionScrollPreservation =
  () => FinishListDeletionScrollPreservation;

const finishWithoutList = () => {};
const beginWithoutList: BeginListDeletionScrollPreservation = () =>
  finishWithoutList;

/**
 * Delete dialogs live below the card that owns them, so they can capture the
 * correct EntityList viewport through context before the card is evicted from
 * Apollo's cache. The returned completion callback remains valid even if that
 * card (and the dialog itself) unmounts while the mutation is in flight.
 */
export const ListDeletionScrollContext =
  createContext<BeginListDeletionScrollPreservation>(beginWithoutList);

export function useListDeletionScrollPreservation() {
  return useContext(ListDeletionScrollContext);
}
