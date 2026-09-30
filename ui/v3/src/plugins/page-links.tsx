import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { Button, buttonVariants } from "@/components/ui/button";
import { getRegisteredNavItems, getRegisteredRoutes } from "./registry";

/** Surface registered pages where users manage the owning plugin. */
export function PluginPageLinks({ pluginId }: { pluginId: string }) {
  const intl = useIntl();
  const routes = getRegisteredRoutes().filter(
    (route) => route.pluginId === pluginId,
  );
  const pages = getRegisteredNavItems().filter(
    (item, index, all) =>
      item.pluginId === pluginId &&
      routes.some((route) => route.path === item.to) &&
      all.findIndex(
        (other) => other.pluginId === pluginId && other.to === item.to,
      ) === index,
  );
  return (
    <div className="flex flex-wrap gap-2">
      {pages.map((page) => (
        <Link
          key={page.to}
          to={page.to}
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          {intl.formatMessage(
            { id: "config.plugins.open_page", defaultMessage: "Open {page}" },
            {
              page:
                typeof page.label === "function"
                  ? page.label(intl)
                  : page.label,
            },
          )}
        </Link>
      ))}
    </div>
  );
}

export function ReloadPluginPages() {
  const intl = useIntl();
  return (
    <div
      className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground"
      role="status"
    >
      {intl.formatMessage({
        id: "config.plugins.reload_pages",
        defaultMessage: "Reload the UI to load updated plugin pages.",
      })}
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={() => window.location.reload()}
      >
        {intl.formatMessage({
          id: "actions.reload_ui",
          defaultMessage: "Reload UI",
        })}
      </Button>
    </div>
  );
}
