import Countries from "i18n-iso-countries";
import type { LocaleData } from "i18n-iso-countries";

export const getLocaleCode = (code: string) => code.slice(0, 2);

export async function registerCountry() {
  const countries = await import("i18n-iso-countries/langs/en.json");
  Countries.registerLocale(countries as LocaleData);
}

export const localeLoader: Record<
  string,
  () => Promise<{ default: Record<string, unknown> }>
> = {
  enGB: () => import("./en-GB.json"),
  enUS: () => import("./en-US.json"),
};

export default localeLoader;
