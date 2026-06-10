import { lazy } from "react";

interface LazyComponentError {
  __lazyComponentError?: true;
}

export const isLazyComponentError = (e: unknown) => {
  return !!(e as LazyComponentError).__lazyComponentError;
};

export const lazyComponent = <Props extends object>(
  factory: () => Promise<{ default: React.FC<Props> }>,
) => {
  return lazy(async () => {
    try {
      return await factory();
    } catch (e) {
      // set flag to identify lazy component loading errors
      (e as LazyComponentError).__lazyComponentError = true;
      throw e;
    }
  });
};
