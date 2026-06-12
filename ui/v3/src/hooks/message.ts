import { useCallback } from "react";
import { useIntl } from "react-intl";

/**
 * Shorthand for the ubiquitous `intl.formatMessage({ id, defaultMessage })`
 * pair used throughout the settings pages. For messages with values
 * (plurals, interpolation), use `useIntl` directly.
 */
export function useMsg() {
  const intl = useIntl();
  return useCallback(
    (id: string, defaultMessage: string) =>
      intl.formatMessage({ id, defaultMessage }),
    [intl],
  );
}
