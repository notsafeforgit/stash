import { toast } from "sonner";
import { useEffect, useState } from "react";
import { useApolloClient } from "@apollo/client/react";
import { RouterProvider } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { Spinner } from "src/components/ui/spinner";
import { QueryError } from "./query-error";
import { createAppRouter, type AppRouter } from "@/router";
import { ensurePluginsLoaded, updatePluginIntl } from "@/plugins/loader";

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
  const [error, setError] = useState<Error>();

  useEffect(() => {
    updatePluginIntl(intl);
    if (router) return;
    let cancelled = false;
    const warn = () =>
      toast.warning(
        intl.formatMessage({
          id: "plugins.load_failed",
          defaultMessage:
            "Some plugins could not be loaded. Reload the page to try again.",
        }),
        { id: "plugin-load-error" },
      );
    const ready = () => {
      if (cancelled) return;
      try {
        setRouter(createAppRouter());
      } catch (reason) {
        console.error("Could not initialize plugin routes", reason);
        warn();
        try {
          setRouter(createAppRouter(false));
        } catch (coreError) {
          setError(
            coreError instanceof Error
              ? coreError
              : new Error(String(coreError)),
          );
        }
      }
    };
    ensurePluginsLoaded({ apollo, intl }).then(
      (issues) => {
        if (cancelled) return;
        if (issues.length) warn();
        ready();
      },
      (error: unknown) => {
        console.error("Plugin startup failed", error);
        if (!cancelled) {
          warn();
          ready();
        }
      },
    );
    return () => {
      cancelled = true;
    };
  }, [apollo, intl, router]);

  if (error)
    return (
      <QueryError
        error={error}
        retry={async () => {
          window.location.reload();
        }}
      />
    );

  if (!router) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background text-foreground">
        <Spinner className="size-10 text-muted-foreground" />
      </div>
    );
  }

  return <RouterProvider router={router} />;
}
