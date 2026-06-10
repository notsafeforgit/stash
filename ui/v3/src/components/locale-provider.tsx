import { useEffect, useState, type PropsWithChildren } from "react";
import { IntlProvider } from "react-intl";
import localeLoader, { registerCountry } from "@/locales";
import flattenMessages from "src/utils/flatten-messages";
import { getPlatformURL } from "src/core/create-client";

export const DEFAULT_LOCALE = "en-GB";

function languageMessageKey(language: string) {
  return language.replace(/-/, "");
}

function translateLanguageLocale(l: string) {
  switch (l) {
    case "nn-NO":
      return "nb-NO";
    default:
      return l;
  }
}

interface LocaleProviderProps extends PropsWithChildren {
  language: string;
}

export function LocaleProvider({ language, children }: LocaleProviderProps) {
  const [messages, setMessages] = useState<Record<string, string>>();
  const intlLanguage = translateLanguageLocale(language);

  useEffect(() => {
    let cancelled = false;

    async function loadMessages() {
      const defaultKey = languageMessageKey(DEFAULT_LOCALE);
      const chosenKey = languageMessageKey(language);

      await registerCountry(language);

      const defaultMsgs = (await localeLoader[defaultKey]()).default;
      const merged: Record<string, unknown> = { ...defaultMsgs };

      if (chosenKey !== defaultKey && localeLoader[chosenKey]) {
        const chosenMsgs = (await localeLoader[chosenKey]()).default;
        // Merge: skip empty strings so default fills in untranslated keys
        function mergeDeep(
          target: Record<string, unknown>,
          source: Record<string, unknown>,
        ) {
          for (const key of Object.keys(source)) {
            const val = source[key];
            if (val === "") continue;
            if (
              typeof val === "object" &&
              val !== null &&
              typeof target[key] === "object" &&
              target[key] !== null
            ) {
              mergeDeep(
                target[key] as Record<string, unknown>,
                val as Record<string, unknown>,
              );
            } else {
              target[key] = val;
            }
          }
        }
        mergeDeep(merged, chosenMsgs);
      }

      // Attempt to load custom locale overrides from server
      try {
        const res = await fetch(getPlatformURL("customlocales").toString());
        if (res.ok) {
          const custom = await res.json();
          function mergeDeep(
            target: Record<string, unknown>,
            source: Record<string, unknown>,
          ) {
            for (const key of Object.keys(source)) {
              const val = source[key];
              if (val === "") continue;
              if (
                typeof val === "object" &&
                val !== null &&
                typeof target[key] === "object" &&
                target[key] !== null
              ) {
                mergeDeep(
                  target[key] as Record<string, unknown>,
                  val as Record<string, unknown>,
                );
              } else {
                target[key] = val;
              }
            }
          }
          mergeDeep(merged, custom);
        }
      } catch {
        // Custom locales are optional
      }

      if (!cancelled) {
        setMessages(
          flattenMessages(merged as Parameters<typeof flattenMessages>[0]),
        );
      }
    }

    loadMessages();
    return () => {
      cancelled = true;
    };
  }, [language]);

  if (!messages) return null;

  return (
    <IntlProvider
      locale={intlLanguage}
      messages={messages}
      defaultLocale={DEFAULT_LOCALE}
    >
      {children}
    </IntlProvider>
  );
}
