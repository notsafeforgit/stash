export const SETTINGS_NAV_ITEMS = [
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
] as const;

export type SettingsNavItem = (typeof SETTINGS_NAV_ITEMS)[number];

/** The longest match also handles settings subpages. */
export function getSettingsSection(pathname: string) {
  return SETTINGS_NAV_ITEMS.filter(
    (item) => pathname === item.to || pathname.startsWith(`${item.to}/`),
  ).sort((a, b) => b.to.length - a.to.length)[0];
}
