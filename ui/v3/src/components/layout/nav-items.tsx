import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import {
  Play,
  Image,
  Film,
  MapPin,
  Images,
  Users,
  Building,
  Tag,
  Download,
} from "lucide-react";
import { getRegisteredNavItems, type NavPlacement } from "@/plugins";

export interface NavItem {
  label: React.ReactNode;
  icon: React.ReactNode;
  to: string;
  hotkey?: string;
}

const BUILTIN_NAV_ITEMS: NavItem[] = [
  {
    label: "Scenes",
    icon: <Play className="size-4" />,
    to: "/scenes",
    hotkey: "g s",
  },
  {
    label: "Images",
    icon: <Image className="size-4" />,
    to: "/images",
    hotkey: "g i",
  },
  {
    label: "Groups",
    icon: <Film className="size-4" />,
    to: "/groups",
    hotkey: "g v",
  },
  {
    label: "Markers",
    icon: <MapPin className="size-4" />,
    to: "/scenes/markers",
    hotkey: "g k",
  },
  {
    label: "Galleries",
    icon: <Images className="size-4" />,
    to: "/galleries",
    hotkey: "g l",
  },
  {
    label: "Performers",
    icon: <Users className="size-4" />,
    to: "/performers",
    hotkey: "g p",
  },
  {
    label: "Studios",
    icon: <Building className="size-4" />,
    to: "/studios",
    hotkey: "g u",
  },
  {
    label: "Tags",
    icon: <Tag className="size-4" />,
    to: "/tags",
    hotkey: "g t",
  },
  {
    // Plain string label here — matches the convention of every other
    // built-in nav item (Scenes / Images / etc. are all hardcoded
    // English too). The corresponding `offline.title` key in en-GB
    // exists so the rest of the offline UI has a translation source,
    // and a future pass that introduces intl-aware built-in labels
    // can pick it up consistently with the others.
    label: "Offline",
    icon: <Download className="size-4" />,
    to: "/offline",
    hotkey: "g o",
  },
];

/**
 * Backwards-compatible export — preserves legacy import sites that
 * referenced NAV_ITEMS as a static array. Plugin nav additions are
 * NOT included here; use `useNavItems({ placement })` instead.
 */
export const NAV_ITEMS: readonly NavItem[] = BUILTIN_NAV_ITEMS;

export function useNavItems(opts?: { placement?: NavPlacement }): NavItem[] {
  const intl = useIntl();
  const placement: NavPlacement = opts?.placement ?? "main";

  const pluginItems = getRegisteredNavItems()
    .filter((item) => (item.placement ?? "main") === placement)
    .map((item) => ({
      label: typeof item.label === "function" ? item.label(intl) : item.label,
      icon: item.icon,
      to: item.to,
      hotkey: item.hotkey,
    }));

  if (placement !== "main") return pluginItems;
  return [...BUILTIN_NAV_ITEMS, ...pluginItems];
}

interface NavLinksProps {
  onClick?: () => void;
  className?: string;
}

export function NavLinks({ onClick, className }: NavLinksProps) {
  const items = useNavItems({ placement: "main" });
  return (
    <ul className={className}>
      {items.map((item) => (
        <li key={item.to}>
          <Link
            to={item.to}
            // Exact match prevents the Scenes link (`/scenes`) from
            // also highlighting on the Markers route
            // (`/scenes/markers`) due to TSR's default prefix matching.
            // `includeSearch: false` so list pages stay highlighted
            // when their URL carries filter params (e.g. ?fa=...).
            activeOptions={{ exact: true, includeSearch: false }}
            onClick={onClick}
            className="flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground [&.active]:bg-accent [&.active]:text-accent-foreground"
          >
            {item.icon}
            {item.label}
          </Link>
        </li>
      ))}
    </ul>
  );
}
