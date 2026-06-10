import Countries from "i18n-iso-countries";
import { getLocaleCode } from "src/locales";

/**
 * Converts an ISO 3166-1 alpha-2 country code to its flag emoji.
 * Works by mapping each letter to the corresponding Regional Indicator Symbol.
 * Returns an empty string for invalid/empty codes.
 */
export function countryCodeToFlag(code: string): string {
  if (code.length !== 2) return "";
  const upper = code.toUpperCase();
  return (
    String.fromCodePoint(upper.charCodeAt(0) + 127397) +
    String.fromCodePoint(upper.charCodeAt(1) + 127397)
  );
}

export const getCountryByISO = (
  iso: string | null | undefined,
  locale: string = "en",
): string | undefined => {
  if (!iso) return;

  const ret = Countries.getName(iso, getLocaleCode(locale));
  if (ret) {
    return ret;
  }

  // fallback to english if locale is not en
  if (locale !== "en") {
    return Countries.getName(iso, "en");
  }
};

export const getCountries = (locale: string = "en") => {
  let countries = Countries.getNames(getLocaleCode(locale));

  if (!countries.length) {
    countries = Countries.getNames("en");
  }

  return Object.entries(countries).map(([code, name]) => ({
    label: name,
    value: code,
  }));
};
