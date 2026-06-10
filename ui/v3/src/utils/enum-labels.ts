import type { IntlShape } from "react-intl";
import { GenderEnum, CircumcisedEnum } from "src/core/generated-graphql";

const GENDER_KEYS = new Set<string>(Object.values(GenderEnum));
const CIRCUMCISED_KEYS = new Set<string>(Object.values(CircumcisedEnum));

/**
 * Best-effort normalisation of a raw string to a known enum key.
 * Scrapers return values in mixed case ("Female", "transgender female")
 * while form values use the API enum constants ("FEMALE",
 * "TRANSGENDER_FEMALE"). We upper-case + replace whitespace/hyphens with
 * underscores so both forms collapse onto the API key.
 */
function normaliseEnumKey(raw: string): string {
  return raw.toUpperCase().replace(/[\s-]/g, "_");
}

/** Format a raw gender string (enum key or scraper-returned label) for display. */
export function formatGender(
  intl: IntlShape,
  raw: string | null | undefined,
): string {
  if (!raw) return "";
  const key = normaliseEnumKey(raw);
  if (GENDER_KEYS.has(key)) {
    return intl.formatMessage({
      id: `gender_types.${key}`,
      defaultMessage: raw,
    });
  }
  return raw;
}

/** Format a raw circumcised string (enum key or scraper label) for display. */
export function formatCircumcised(
  intl: IntlShape,
  raw: string | null | undefined,
): string {
  if (!raw) return "";
  const key = normaliseEnumKey(raw);
  if (CIRCUMCISED_KEYS.has(key)) {
    return intl.formatMessage({
      id: `circumcised_types.${key}`,
      defaultMessage: raw,
    });
  }
  return raw;
}
