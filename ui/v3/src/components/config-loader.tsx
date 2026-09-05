import { useQuery } from "@apollo/client/react";
import type { PropsWithChildren } from "react";
import { ConfigurationDocument } from "@/core/generated-graphql";
import { ConfigurationProvider } from "src/hooks/config";
import { LocaleProvider, DEFAULT_LOCALE } from "./locale-provider";
import { Spinner } from "src/components/ui/spinner";
import { QueryError, StartupError } from "./query-error";

/**
 * Fetches server configuration and wires up ConfigurationProvider + LocaleProvider.
 * Keeps the app mounted during background refreshes and offers recovery when
 * initial configuration cannot be fetched.
 */
export function ConfigLoader({ children }: PropsWithChildren) {
  const { data, error, loading, refetch } = useQuery(ConfigurationDocument);

  if (!data) {
    if (error)
      return <StartupError error={error} retry={refetch} retrying={loading} />;
    return (
      <div className="flex min-h-screen items-center justify-center bg-background text-foreground">
        <Spinner className="size-10 text-muted-foreground" />
      </div>
    );
  }

  const language = data.configuration.interface.language ?? DEFAULT_LOCALE;

  return (
    <LocaleProvider language={language}>
      <ConfigurationProvider configuration={data.configuration}>
        {error && (
          <QueryError error={error} retry={refetch} retrying={loading} stale />
        )}
        {children}
      </ConfigurationProvider>
    </LocaleProvider>
  );
}
