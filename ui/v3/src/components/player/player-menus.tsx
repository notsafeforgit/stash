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
import { getQualityMenuSources } from "./player-quality-options";

type PlayerInstance = CreatePlayerResult<VideoPlayerStore>;

// ── Speed selector ────────────────────────────────────────────────────────────

const PLAYBACK_SPEEDS = [2, 1.75, 1.5, 1.25, 1, 0.75, 0.5, 0.25];

export function SpeedMenu({
  Player,
  onOpenChange,
  className,
}: {
  Player: PlayerInstance;
  onOpenChange?: (open: boolean) => void;
  className?: string;
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
            className={cn(
              "rounded bg-transparent px-1.5 text-xs font-medium tabular-nums text-white/80 hover:bg-white/10 hover:text-white",
              className,
            )}
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
  className?: string;
}

export function QualityMenu({
  sources,
  activeSource,
  onSourceChange,
  sourceResolution,
  onOpenChange,
  className,
}: QualityMenuProps) {
  const choices = getQualityMenuSources(sources, activeSource);

  // Even a sole source must be selectable if the remembered ceiling left
  // playback unselected and the user needs an explicit override.
  if (!choices.length || (choices.length === 1 && activeSource)) return null;
  const activeKey = activeSource?.src ?? "";

  function handleValueChange(key: string) {
    const next = choices.find((s) => s.src === key);
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
            className={cn(
              "rounded bg-transparent text-white/80 hover:bg-white/10 hover:text-white",
              className,
            )}
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
          {choices.map((s) => {
            const key = s.src;
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
