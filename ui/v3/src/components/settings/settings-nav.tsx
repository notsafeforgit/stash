import { useIntl } from "react-intl";
import { Link, useRouterState } from "@tanstack/react-router";
import { cn } from "@/lib/utils";

interface NavItem {
  to: string;
  labelId: string;
  defaultLabel: string;
}

const ITEMS: NavItem[] = [
  {
    to: "/settings/tasks",
    labelId: "config.categories.tasks",
    defaultLabel: "Tasks",
  },
  {
    to: "/settings/library",
    labelId: "library",
    defaultLabel: "Library",
  },
  {
    to: "/settings/interface",
    labelId: "config.categories.interface",
    defaultLabel: "Interface",
  },
  {
    to: "/settings/security",
    labelId: "config.categories.security",
    defaultLabel: "Security",
  },
  {
    to: "/settings/metadata-providers",
    labelId: "config.categories.metadata_providers",
    defaultLabel: "Metadata Providers",
  },
  {
    to: "/settings/services",
    labelId: "config.categories.services",
    defaultLabel: "Services",
  },
  {
    to: "/settings/system",
    labelId: "config.categories.system",
    defaultLabel: "System",
  },
  {
    to: "/settings/plugins",
    labelId: "config.categories.plugins",
    defaultLabel: "Plugins",
  },
  {
    to: "/settings/logs",
    labelId: "config.categories.logs",
    defaultLabel: "Logs",
  },
  {
    to: "/settings/tools",
    labelId: "config.categories.tools",
    defaultLabel: "Tools",
  },
  {
    to: "/settings/about",
    labelId: "config.categories.about",
    defaultLabel: "About",
  },
];

function isItemActive(item: NavItem, pathname: string) {
  return pathname === item.to || pathname.startsWith(`${item.to}/`);
}

export function SettingsNav() {
  const intl = useIntl();
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  function renderItem(item: NavItem, mobile: boolean) {
    const isActive = isItemActive(item, pathname);
    return (
      <Link
        key={item.to}
        to={item.to}
        className={cn(
          "rounded-md px-3 text-sm font-medium transition-colors",
          mobile ? "whitespace-nowrap py-1.5" : "py-2",
          isActive
            ? "bg-muted text-foreground"
            : "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
        )}
      >
        {intl.formatMessage({
          id: item.labelId,
          defaultMessage: item.defaultLabel,
        })}
      </Link>
    );
  }

  return (
    <>
      {/* Desktop: vertical sidebar */}
      <nav className="hidden md:flex md:w-56 md:shrink-0 md:flex-col md:gap-1 md:border-r md:pr-3">
        {ITEMS.map((item) => renderItem(item, false))}
      </nav>

      {/* Mobile: horizontal scrolling tabs */}
      <nav className="flex shrink-0 gap-1 overflow-x-auto border-b px-4 py-2 md:hidden">
        {ITEMS.map((item) => renderItem(item, true))}
      </nav>
    </>
  );
}
