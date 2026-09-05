import { withTimeout } from "@/utils/with-timeout";
import { StartupError } from "./query-error";
import { Spinner } from "./ui/spinner";
import {
  useCallback,
  useEffect,
  useState,
  type PropsWithChildren,
} from "react";
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

export function LocaleProvider(props: LocaleProviderProps) {
  const [attempt, setAttempt] = useState(0);
  const retry = useCallback(async () => setAttempt((n) => n + 1), []);
  return <LocaleLoader key={attempt} {...props} retry={retry} />;
}

function LocaleLoader({
  language,
  children,
  retry,
}: LocaleProviderProps & { retry: () => Promise<void> }) {
  const [messages, setMessages] = useState<Record<string, string>>();
  const [error, setError] = useState<Error>();
  const intlLanguage = translateLanguageLocale(language);

  useEffect(() => {
    let cancelled = false;
    const abort = new AbortController();
    setError(undefined);

    async function loadMessages() {
      const defaultKey = languageMessageKey(DEFAULT_LOCALE);
      const chosenKey = languageMessageKey(language);

      await withTimeout(registerCountry(language), 5000, "Country names");

      const defaultMsgs = (
        await withTimeout(localeLoader[defaultKey](), 5000, "Default language")
      ).default;
      const merged: Record<string, unknown> = { ...defaultMsgs };

      if (chosenKey !== defaultKey && localeLoader[chosenKey]) {
        const chosenMsgs = (
          await withTimeout(
            localeLoader[chosenKey](),
            5000,
            "Selected language",
          )
        ).default;
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

      if (!cancelled)
        setMessages(
          flattenMessages(merged as Parameters<typeof flattenMessages>[0]),
        );

      // Optional custom overrides must not delay the core app.
      try {
        const res = await withTimeout(
          fetch(getPlatformURL("customlocales"), { signal: abort.signal }),
          5000,
          "Custom language",
        );
        if (res.ok) {
          const custom = await withTimeout(
            res.json(),
            5000,
            "Custom language body",
          );
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
        // Custom locales are optional.
      } finally {
        abort.abort();
      }

      if (!cancelled) {
        setMessages(
          flattenMessages(merged as Parameters<typeof flattenMessages>[0]),
        );
      }
    }

    void loadMessages().catch((reason: unknown) => {
      if (!cancelled)
        setError(reason instanceof Error ? reason : new Error(String(reason)));
    });
    return () => {
      cancelled = true;
      abort.abort();
    };
  }, [language]);

  if (!messages && error) return <StartupError error={error} retry={retry} />;
  if (!messages)
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Spinner className="size-10" />
      </div>
    );

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
