import { useCallback, useEffect, useMemo, useRef, useState } from "react";

export interface DebounceSettings {
  leading?: boolean;
  trailing?: boolean;
  maxWait?: number;
}

// The loosest function shape every concrete function is assignable to:
// `never[]` parameters are contravariant-permissive and `unknown` accepts
// any return type — unlike `any`, neither disables type checking.
type AnyFunction = (...args: never[]) => unknown;

export type DebouncedFunc<T extends AnyFunction> = T & {
  cancel: () => void;
  flush: () => ReturnType<T> | undefined;
};

function debounce<T extends AnyFunction>(
  fn: T,
  wait = 0,
  _options?: DebounceSettings,
): DebouncedFunc<T> {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let lastThis: unknown;
  let lastArgs: Parameters<T>;

  const debounced = function (this: unknown, ...args: Parameters<T>) {
    // eslint-disable-next-line @typescript-eslint/no-this-alias
    lastThis = this;
    lastArgs = args;
    if (timer !== null) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = null;
      fn.apply(lastThis, lastArgs);
    }, wait);
  } as DebouncedFunc<T>;

  debounced.cancel = () => {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  };

  debounced.flush = (): ReturnType<T> | undefined => {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
      // `apply` on the generic `T` erases the return type to `unknown`;
      // calling `fn` with `Parameters<T>` is what defines `ReturnType<T>`.
      return fn.apply(lastThis, lastArgs) as ReturnType<T>;
    }
  };

  return debounced;
}

export function useDebounce<T extends AnyFunction>(
  fn: T,
  wait?: number,
  options?: DebounceSettings,
): DebouncedFunc<T> {
  const func = useRef<T>(fn);
  func.current = fn;
  const leading = options?.leading;
  const trailing = options?.trailing;
  const maxWait = options?.maxWait;
  // func is a ref — always reflects the latest fn without re-creating the debounce.
  return useMemo(
    () =>
      debounce(
        function (this: unknown, ...args: Parameters<T>) {
          return func.current.apply(this, args);
        } as T,
        wait,
        { leading, trailing, maxWait },
      ),
    [wait, leading, trailing, maxWait],
  );
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
      setDisplayedState(v);
      setValue(v);
    },
    [setValue],
  );

  return [displayedState, onChange, setInstant];
}
