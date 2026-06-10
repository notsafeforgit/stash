import { useEffect, useState } from "react";
import { useApolloClient } from "@apollo/client/react";
import { RouterProvider } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { Spinner } from "src/components/ui/spinner";
import { createAppRouter, type AppRouter } from "@/router";
import { ensurePluginsLoaded } from "@/plugins/loader";

/**
 * Runs the plugin loader once on mount, then mounts the router with
 * plugin-registered routes and nav items merged in. Sits inside
 * ApolloProvider + IntlProvider so the loader has access to a fully
 * configured Apollo client and intl shape.
 */
export function PluginLoader() {
  const apollo = useApolloClient();
  const intl = useIntl();
  const [router, setRouter] = useState<AppRouter | null>(null);

  useEffect(() => {
    let cancelled = false;
    ensurePluginsLoaded({ apollo, intl }).then(() => {
      if (cancelled) return;
      setRouter(createAppRouter());
    });
    return () => {
      cancelled = true;
    };
  }, [apollo, intl]);

  if (!router) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background text-foreground">
        <Spinner className="size-10 text-muted-foreground" />
      </div>
    );
  }

  return <RouterProvider router={router} />;
}
