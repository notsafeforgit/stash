import { useCallback, useEffect, useMemo, useState } from "react";
import debounce from "lodash-es/debounce";
import type { DebouncedFunc } from "lodash-es";
import { useCommittedRef } from "./use-committed-ref";

export type { DebouncedFunc } from "lodash-es";

export interface DebounceSettings {
  leading?: boolean;
  trailing?: boolean;
  maxWait?: number;
  /** Explicit opt-in for persisted edits; ordinary delayed work is cancelled. */
  flushOnUnmount?: boolean;
}

export function useDebounce<Args extends unknown[], Result>(
  fn: (...args: Args) => Result,
  wait = 0,
  options: DebounceSettings = {},
): DebouncedFunc<(...args: Args) => Result> {
  const callback = useCommittedRef(fn);
  const {
    leading = false,
    trailing = true,
    maxWait,
    flushOnUnmount = false,
  } = options;
  const debounced = useMemo(
    () =>
      debounce((...args: Args) => callback.current(...args), wait, {
        leading,
        trailing,
        ...(maxWait === undefined ? {} : { maxWait }),
      }),
    [wait, leading, trailing, maxWait],
  );
  useEffect(
    () => () => {
      if (flushOnUnmount) debounced.flush();
      debounced.cancel();
    },
    [debounced, flushOnUnmount],
  );
  return debounced;
}

/**
 * Returns a debounced copy of `value` that only updates after `wait` ms of
 * the input being stable. Useful for delaying expensive operations (like
 * queries) while keeping the source value immediately responsive.
 */
export function useDebouncedValue<T>(value: T, wait: number): T {
  const [debounced, setDebounced] = useState(value);

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), wait);
    return () => clearTimeout(timer);
  }, [value, wait]);

  return debounced;
}

export function useDebouncedState<T>(
  initialValue: T,
  setValue: (v: T) => void,
  wait?: number,
): [T, (v: T) => void, (v: T) => void] {
  const [displayedState, setDisplayedState] = useState(initialValue);

  const debouncedSetValue = useDebounce(setValue, wait);
  const onChange = useCallback(
    (input: T) => {
      setDisplayedState(input);
      debouncedSetValue(input);
    },
    [debouncedSetValue],
  );

  const setInstant = useCallback(
    (v: T) => {
      debouncedSetValue.cancel();
      setDisplayedState(v);
      setValue(v);
    },
    [setValue, debouncedSetValue],
  );

  return [displayedState, onChange, setInstant];
}
