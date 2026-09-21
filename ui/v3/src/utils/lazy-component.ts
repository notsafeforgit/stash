import { lazy } from "react";

interface LazyComponentError {
  __lazyComponentError?: true;
}

export const isLazyComponentError = (e: unknown) => {
  return (
    e instanceof Error &&
    "__lazyComponentError" in e &&
    e.__lazyComponentError === true
  );
};

export const lazyComponent = <Props extends object>(
  factory: () => Promise<{ default: React.FC<Props> }>,
) => {
  return lazy(async () => {
    try {
      return await factory();
    } catch (e) {
      // set flag to identify lazy component loading errors
      const error = e instanceof Error ? e : new Error(String(e), { cause: e });
      throw Object.assign(error, {
        __lazyComponentError: true,
      } satisfies LazyComponentError);
    }
  });
};
