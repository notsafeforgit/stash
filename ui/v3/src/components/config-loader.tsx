import { useQuery } from "@apollo/client/react";
import type { PropsWithChildren } from "react";
import { ConfigurationDocument } from "@/core/generated-graphql";
import { ConfigurationProvider } from "src/hooks/config";
import { LocaleProvider, DEFAULT_LOCALE } from "./locale-provider";
import { Spinner } from "src/components/ui/spinner";

/**
 * Fetches server configuration and wires up ConfigurationProvider + LocaleProvider.
 * Renders nothing until configuration is available.
 */
export function ConfigLoader({ children }: PropsWithChildren) {
  const { data, loading } = useQuery(ConfigurationDocument);

  if (loading || !data) {
    // Minimal loading state — no locale needed yet
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
        {children}
      </ConfigurationProvider>
    </LocaleProvider>
  );
}
