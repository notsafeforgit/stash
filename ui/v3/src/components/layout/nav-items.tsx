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
  Tv,
  Puzzle,
  Link2,
  FolderOpen,
  HardDrive,
  FileText,
  History,
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
  { label: "TV", icon: <Tv className="size-4" />, to: "/tv", hotkey: "g r" },
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

export function useNavItems(opts?: {
  placement?: NavPlacement | readonly NavPlacement[];
}): NavItem[] {
  const intl = useIntl();
  const placement = opts?.placement ?? "main";
  const placements = typeof placement === "string" ? [placement] : placement;

  const pluginItems = getRegisteredNavItems()
    .filter((item) => placements.includes(item.placement ?? "main"))
    .map((item) => ({
      label: typeof item.label === "function" ? item.label(intl) : item.label,
      icon: item.icon ?? <Puzzle className="size-4" />,
      to: item.to,
      hotkey: item.hotkey,
    }));

  const archiveItems: NavItem[] = placements.includes("utility")
    ? [
        {
          label: intl.formatMessage({
            id: "archive_activity.title",
            defaultMessage: "Archive activity",
          }),
          icon: <History className="size-4" />,
          to: "/archive-activity",
        },
        {
          label: intl.formatMessage({
            id: "import_history.title",
            defaultMessage: "Import history",
          }),
          icon: <History className="size-4" />,
          to: "/import-history",
        },
        {
          label: intl.formatMessage({
            id: "saved_actions.title",
            defaultMessage: "Saved actions",
          }),
          icon: <History className="size-4" />,
          to: "/saved-actions",
        },
        {
          label: intl.formatMessage({
            id: "source_posts.title",
            defaultMessage: "Source posts",
          }),
          icon: <FileText className="size-4" />,
          to: "/source-posts",
        },
        {
          label: intl.formatMessage({
            id: "collections.title",
            defaultMessage: "Source collections",
          }),
          icon: <FolderOpen className="size-4" />,
          to: "/collections",
        },
        {
          label: intl.formatMessage({
            id: "media_roots.title",
            defaultMessage: "Media roots",
          }),
          icon: <HardDrive className="size-4" />,
          to: "/media-roots",
        },
        {
          label: intl.formatMessage({
            id: "account_review.title",
            defaultMessage: "Account review",
          }),
          icon: <Link2 className="size-4" />,
          to: "/account-review",
        },
      ]
    : [];
  const items = [
    ...(placements.includes("main") ? BUILTIN_NAV_ITEMS : []),
    ...archiveItems,
    ...pluginItems,
  ];
  // A destination registered for several surfaces appears once in a combined menu.
  return items.filter(
    (item, index) => items.findIndex((other) => other.to === item.to) === index,
  );
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
