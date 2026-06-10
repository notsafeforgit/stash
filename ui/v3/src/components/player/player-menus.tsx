/**
 * Speed and quality dropdown menus for the scene player. Split out of
 * `player-controls.tsx` so the controls overlay file can stay focused
 * on layout, hotkeys, and the time/touch interaction model.
 */
import type { CreatePlayerResult, VideoPlayerStore } from "@videojs/react";
import { Settings } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "src/components/ui/dropdown-menu";
import { Button } from "src/components/ui/button";
import { cn } from "src/lib/utils";
import type { PlayerSource } from "./player-utils";

type PlayerInstance = CreatePlayerResult<VideoPlayerStore>;

// ── Speed selector ────────────────────────────────────────────────────────────

const PLAYBACK_SPEEDS = [2, 1.75, 1.5, 1.25, 1, 0.75, 0.5, 0.25];

export function SpeedMenu({
  Player,
  onOpenChange,
}: {
  Player: PlayerInstance;
  onOpenChange?: (open: boolean) => void;
}) {
  const rate = Player.usePlayer((s) => s.playbackRate);
  const store = Player.usePlayer();

  // `modal={false}`: Base UI's default modal behaviour locks document
  // scroll and inserts a scrollbar-gutter compensator on body, which
  // on mobile triggers a viewport recalc around the address bar and
  // produces a black flash + content shift inside YARL's lightbox.
  // Outside-click suppression for the player's tap-to-play handler
  // is already handled by `isMenuActive()` upstream, so we don't
  // need the modal scroll lock or the InternalBackdrop overlay here.
  return (
    <DropdownMenu modal={false} onOpenChange={onOpenChange}>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            className="rounded bg-transparent px-1.5 text-xs font-medium tabular-nums text-white/80 hover:bg-white/10 hover:text-white"
            aria-label="Playback speed"
          />
        }
      >
        {rate}x
      </DropdownMenuTrigger>
      <DropdownMenuContent
        side="top"
        align="end"
        sideOffset={6}
        positionerClassName="z-[10000]"
        className="min-w-[6em] border-0 bg-black/90 text-white shadow-lg ring-0"
      >
        <DropdownMenuRadioGroup
          value={String(rate)}
          onValueChange={(v) => store.setPlaybackRate(Number(v))}
        >
          {PLAYBACK_SPEEDS.map((s) => (
            <DropdownMenuRadioItem
              key={s}
              value={String(s)}
              closeOnClick
              className={cn(
                "text-white/70 focus:bg-white/10 focus:text-white",
                s === rate && "font-medium text-white",
              )}
            >
              {s}x
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// ── Quality menu ──────────────────────────────────────────────────────────────
// Source/quality picker behind the gear icon. Loop and auto-advance are
// exposed as top-level control bar buttons instead — they're per-scene /
// session toggles users flip more often than they pick a quality.

interface QualityMenuProps {
  sources: PlayerSource[];
  activeSource: PlayerSource | null;
  onSourceChange: (source: PlayerSource) => void;
  /** Source resolution label (e.g. "1080p"). Appended to the
   *  "Direct stream" row so the user can see the actual resolution
   *  they're getting alongside the resolution-suffixed transcode rows. */
  sourceResolution?: string;
  onOpenChange?: (open: boolean) => void;
}

export function QualityMenu({
  sources,
  activeSource,
  onSourceChange,
  sourceResolution,
  onOpenChange,
}: QualityMenuProps) {
  // Hide rows that would re-deliver the best-quality non-loss tier at
  // identical effective quality:
  //   - When raw direct stream is offered, "HLS (remux)" is redundant
  //     (codec-copy fMP4 passthrough — same bytes, different container).
  //   - "HLS (transcode)" / "MP4" / "WEBM" are the bare-format labels
  //     the backend emits for StreamingResolutionEnum.Original — i.e.
  //     transcodes targeting the source's own resolution. They buy
  //     nothing over either direct streaming or the remux. Hidden
  //     whenever any best-quality non-loss row is available.
  // Lower-resolution rows ("MP4 HD (720p)", "HLS Full HD (1080p)" etc.)
  // stay visible — they're genuine smaller-bitrate alternatives.
  //
  // The hide is skipped when the active source is one of the otherwise-
  // hidden rows, so a user who explicitly picked it on a prior scene
  // still sees their selection highlighted.
  const hasDirect = sources.some((s) => s.label === "Direct stream");
  const hasRemux = sources.some((s) => s.label === "HLS (remux)");
  const activeLabel = activeSource?.label;

  const redundantLabels = new Set<string>();
  if (hasDirect) redundantLabels.add("HLS (remux)");
  if (hasDirect || hasRemux) {
    redundantLabels.add("HLS (transcode)");
    redundantLabels.add("MP4");
    redundantLabels.add("WEBM");
  }

  const visibleSources = sources.filter((s) => {
    if (!s.label) return true;
    if (!redundantLabels.has(s.label)) return true;
    return s.label === activeLabel;
  });

  // Standard label-dedup for the rest. Order-preserving: first
  // occurrence wins. Unlabeled sources are always kept.
  const deduped = visibleSources.filter((s, i) => {
    if (!s.label) return true;
    return visibleSources.findIndex((o) => o.label === s.label) === i;
  });

  if (deduped.length <= 1) return null;

  // Active-row match: by label when labelled, otherwise fall back to
  // reference equality via `src`.
  const activeKey = activeSource?.label ?? activeSource?.src ?? "";

  function handleValueChange(key: string) {
    const next = deduped.find((s) => (s.label ?? s.src) === key) ?? deduped[0];
    if (next) onSourceChange(next);
  }

  // See SpeedMenu for the `modal={false}` rationale — mobile flash
  // avoidance + outside-click suppression handled upstream.
  return (
    <DropdownMenu modal={false} onOpenChange={onOpenChange}>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon-lg"
            className="rounded bg-transparent text-white/80 hover:bg-white/10 hover:text-white"
            aria-label="Quality"
          />
        }
      >
        <Settings />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        side="top"
        align="end"
        sideOffset={6}
        positionerClassName="z-[10000]"
        className="min-w-[12em] border-0 bg-black/90 text-white shadow-lg ring-0"
      >
        <DropdownMenuRadioGroup
          value={activeKey}
          onValueChange={handleValueChange}
        >
          {deduped.map((s) => {
            const key = s.label ?? s.src;
            const isActive = key === activeKey;
            // Source resolution suffix on the best-quality non-loss
            // tier (direct or remux) — both deliver the file at its
            // native resolution, and showing it lets the user compare
            // against the resolution-suffixed transcode rows.
            const isBestQualityRow =
              s.label === "Direct stream" || s.label === "HLS (remux)";
            const display =
              isBestQualityRow && sourceResolution
                ? `${s.label} (${sourceResolution})`
                : (s.label ?? s.type ?? s.src);
            return (
              <DropdownMenuRadioItem
                key={s.src}
                value={key}
                closeOnClick
                className={cn(
                  "text-white/70 focus:bg-white/10 focus:text-white",
                  isActive && "font-medium text-white",
                )}
              >
                {display}
              </DropdownMenuRadioItem>
            );
          })}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
