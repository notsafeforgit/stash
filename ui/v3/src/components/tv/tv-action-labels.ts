import {
  Settings,
  Eye,
  Info,
  Star,
  Droplets,
  Check,
  Tags,
  Tag,
  BookmarkPlus,
  Bookmark,
  Trash2,
  RotateCw,
  Maximize,
  Volume2,
  Expand,
  Repeat,
  Gauge,
  Captions,
  SlidersHorizontal,
  CircleHelp,
  Heart,
  Flame,
  Sparkles,
  Plus,
  Folder,
  ListVideo,
  type LucideIcon,
} from "lucide-react";
import type { TvAction, TvActionKind } from "@/core/tv/action-config";
import type { TvMode } from "@/core/tv/settings";
import type { MessageDescriptor } from "react-intl";

export const tvModeLabels = {
  scenes: { id: "tv.text.scenes", defaultMessage: "Scenes" },
  markers: { id: "tv.text.markers", defaultMessage: "Markers" },
} satisfies Record<TvMode, MessageDescriptor>;

export const tvFeedSwitchLabels = {
  scenes: {
    id: "tv.feed.switch_to_markers",
    defaultMessage: "Switch to markers",
  },
  markers: {
    id: "tv.feed.switch_to_scenes",
    defaultMessage: "Switch to scenes",
  },
} satisfies Record<TvMode, MessageDescriptor>;

export const tvIconLabels = {
  default: { id: "tv.icon.default", defaultMessage: "Default" },
  heart: { id: "tv.icon.heart", defaultMessage: "Heart" },
  star: { id: "tv.icon.star", defaultMessage: "Star" },
  tag: { id: "tv.icon.tag", defaultMessage: "Tag" },
  bookmark: { id: "tv.icon.bookmark", defaultMessage: "Bookmark" },
  flame: { id: "tv.icon.flame", defaultMessage: "Flame" },
  sparkles: { id: "tv.icon.sparkles", defaultMessage: "Sparkles" },
  check: { id: "tv.icon.check", defaultMessage: "Check mark" },
  plus: { id: "tv.icon.plus", defaultMessage: "Plus" },
} satisfies Record<TvAction["icon"], MessageDescriptor>;

export const tvActionLabels: Record<TvActionKind, string> = {
  settings: "TV settings",
  feed: "Feed",
  visibility: "Show or hide controls",
  info: "Information",
  rating: "Rating",
  counter: "O-counter",
  organized: "Organized",
  tags: "Edit tags",
  "quick-tag": "Quick tag",
  marker: "Create marker",
  "quick-marker": "Quick marker",
  delete: "Delete current item",
  rotation: "Rotate presentation",
  fullscreen: "Fullscreen",
  volume: "Volume",
  fit: "Fit or fill",
  completion: "Playback mode",
  speed: "Playback speed",
  subtitles: "Subtitles",
  quality: "Quality",
  help: "TV guide",
};
const icons: Record<TvActionKind, LucideIcon> = {
  settings: Settings,
  feed: ListVideo,
  visibility: Eye,
  info: Info,
  rating: Star,
  counter: Droplets,
  organized: Check,
  tags: Tags,
  "quick-tag": Tag,
  marker: BookmarkPlus,
  "quick-marker": Bookmark,
  delete: Trash2,
  rotation: RotateCw,
  fullscreen: Maximize,
  volume: Volume2,
  fit: Expand,
  completion: Repeat,
  speed: Gauge,
  subtitles: Captions,
  quality: SlidersHorizontal,
  help: CircleHelp,
};
export const tvCustomIcons = {
  default: Folder,
  heart: Heart,
  star: Star,
  tag: Tag,
  bookmark: Bookmark,
  flame: Flame,
  sparkles: Sparkles,
  check: Check,
  plus: Plus,
} satisfies Record<TvAction["icon"], LucideIcon>;
export function tvActionIcon(action: TvAction, feedMode?: TvMode): LucideIcon {
  if (action.icon !== "default") return tvCustomIcons[action.icon];
  if (action.kind === "feed" && feedMode === "markers") return Bookmark;
  return icons[action.kind];
}
