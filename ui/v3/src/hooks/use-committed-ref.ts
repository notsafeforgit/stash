import { useLayoutEffect, useRef } from "react";

/** For imperative callbacks and cleanup only. Render from the value itself.
 * Updates become visible after commit, so abandoned renders cannot publish
 * their values to listeners belonging to the current UI. */
export function useCommittedRef<T>(value: T) {
  const ref = useRef(value);
  useLayoutEffect(() => {
    ref.current = value;
  });
  return ref;
}
