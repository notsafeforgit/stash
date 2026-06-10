/**
 * Country picker — combobox over ISO 3166-1 alpha-2 codes with the
 * country flag emoji rendered next to each name. Used as the
 * `country` field on the performer edit form (and any future entity
 * edit form that picks a country).
 *
 * Stored value is the 2-letter ISO code (matches the performer
 * record's `country` field convention shared with v2.5 and the
 * scrapers). Display formatting — flag + localised name — is
 * recomputed in `<CountryDisplay>` and elsewhere from the same
 * `getCountryByISO` / `countryCodeToFlag` helpers.
 */
import { useMemo } from "react";
import { useIntl } from "react-intl";
import {
  Combobox,
  ComboboxCollection,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "src/components/ui/combobox";
import {
  countryCodeToFlag,
  getCountries,
  getCountryByISO,
} from "src/utils/country";

export interface CountrySelectProps {
  /** ISO 3166-1 alpha-2 code (e.g. "US"). Empty / null means no
   *  selection. Legacy free-text values (pre-ISO migration) display
   *  as-is in the input but won't match any combobox option. */
  value: string;
  onChange: (next: string) => void;
  id?: string;
  disabled?: boolean;
  placeholder?: string;
}

export function CountrySelect({
  value,
  onChange,
  id,
  disabled,
  placeholder,
}: CountrySelectProps) {
  const intl = useIntl();
  const countries = useMemo(() => getCountries(intl.locale), [intl.locale]);
  const codes = useMemo(() => countries.map((c) => c.value), [countries]);

  // Built-in filter matches items whose stringified label includes
  // the typed query (case-insensitive). For countries that's name
  // matching; codes themselves are short enough that ISO matches
  // come for free when the user types e.g. "us" (matches both
  // "United States" and "US" in the label string).
  const itemToStringLabel = (code: string) => {
    const flag = countryCodeToFlag(code);
    const name = getCountryByISO(code, intl.locale) ?? code;
    // Include both flag and code so typing the ISO code (e.g. "US")
    // narrows down quickly without forcing the user to spell out
    // "United States".
    return `${flag} ${name} (${code})`;
  };

  return (
    <Combobox<string>
      value={value || null}
      onValueChange={(v) => onChange(v ?? "")}
      items={codes}
      itemToStringLabel={itemToStringLabel}
      disabled={disabled}
    >
      <ComboboxInput
        id={id}
        placeholder={
          placeholder ??
          intl.formatMessage({
            id: "actions.search",
            defaultMessage: "Search…",
          })
        }
        showClear={!!value}
      />
      <ComboboxContent>
        <ComboboxList>
          {/* `<ComboboxCollection>` walks the *filtered* item list
              produced by Base UI's collator-based filter (driven by
              `itemToStringLabel`). Mapping `countries.map(...)`
              statically would render all 250 entries up-front and
              the typed-input filter would do nothing — items would
              be shown regardless of query. */}
          <ComboboxCollection>
            {(code: string) => (
              <ComboboxItem key={code} value={code}>
                <span
                  aria-hidden
                  className="text-base leading-none w-5 shrink-0"
                >
                  {countryCodeToFlag(code)}
                </span>
                <span className="min-w-0 truncate">
                  {getCountryByISO(code, intl.locale) ?? code}
                </span>
                <span className="ml-auto text-xs text-muted-foreground tabular-nums shrink-0">
                  {code}
                </span>
              </ComboboxItem>
            )}
          </ComboboxCollection>
          <ComboboxEmpty>
            {intl.formatMessage({
              id: "no_results_found",
              defaultMessage: "No results found",
            })}
          </ComboboxEmpty>
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}

/**
 * Read-only "{flag} {name}" rendering of a country code, used in
 * detail views. Falls back to the raw value if the code doesn't
 * resolve (legacy free-text data from older imports).
 */
export function CountryDisplay({
  value,
}: {
  value: string | null | undefined;
}) {
  const intl = useIntl();
  if (!value) return null;
  const name = getCountryByISO(value, intl.locale);
  const flag = countryCodeToFlag(value);
  if (!name) return <span>{value}</span>;
  return (
    <span className="inline-flex items-baseline gap-1.5">
      <span aria-hidden className="text-base leading-none">
        {flag}
      </span>
      <span>{name}</span>
    </span>
  );
}

/**
 * String formatter for contexts that compose multiple metadata
 * values into one line (e.g. card subtitles built with `Array.join`).
 * Returns "{flag} {name}" or the raw value when the code doesn't
 * resolve.
 */
export function formatCountry(
  value: string | null | undefined,
  locale: string,
): string {
  if (!value) return "";
  const name = getCountryByISO(value, locale);
  if (!name) return value;
  const flag = countryCodeToFlag(value);
  return flag ? `${flag} ${name}` : name;
}
