import React from "react";

// usePrevious is a hook that returns the previous value of a variable.
export function usePrevious<T>(value: T) {
  const ref = React.useRef<T | undefined>(undefined);
  React.useEffect(() => {
    ref.current = value;
  }, [value]);
  return ref.current;
}
