/**
 * Marker overlay for the time slider.
 *
 * Renders dot markers and range markers as absolutely-positioned elements
 * inside a wrapper that sits just above the slider track. Each marker
 * carries a cursor-tracking tooltip showing its title.
 *
 * Range markers that overlap are pushed to successive layers via the MWIS
 * algorithm so they never visually collide.
 */
import { useMemo } from "react";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "src/components/ui/tooltip";
import { useConfigurationContextOptional } from "src/hooks/config";
import type { IMarker } from "./player-utils";
import { computeTagColors, findMWIS } from "./player-utils";

interface PlayerMarkersProps {
  markers: IMarker[];
  duration: number;
}

// Stack offset between successive range-marker layers. Equals the marker
// visual height (7px) plus the same 2px gap used between the bar and the
// first marker layer (see player-controls.tsx, where the markers wrapper
// is positioned at `bottom: calc(100% + 2px)`).
const LAYER_HEIGHT_PX = 9;
const DOT_R = 3; // half-width of dot in px

export function PlayerMarkers({ markers, duration }: PlayerMarkersProps) {
  // Honor the "Show range markers" UI setting (default on). When off,
  // every marker collapses to a dot at its start time.
  const ctx = useConfigurationContextOptional();
  const showRange = ctx?.configuration.ui.showRangeMarkers ?? true;

  const tagColors = useMemo(() => {
    const tagNames = [
      ...new Set(markers.map((m) => m.primaryTag.name).filter(Boolean)),
    ] as string[];
    return computeTagColors(tagNames);
  }, [markers]);

  const dotMarkers = useMemo(
    () => (showRange ? markers.filter((m) => !m.end_seconds) : markers),
    [markers, showRange],
  );

  const rangeMarkers = useMemo(
    () => (showRange ? markers.filter((m) => !!m.end_seconds) : []),
    [markers, showRange],
  );

  // Assign each range marker a layer index via repeated MWIS
  const layeredRanges = useMemo(() => {
    const result: { marker: IMarker; layer: number }[] = [];
    let remaining = [...rangeMarkers];
    let layer = 0;
    while (remaining.length > 0) {
      const chosen = findMWIS(remaining);
      if (!chosen.length) break;
      for (const m of chosen) result.push({ marker: m, layer });
      remaining = remaining.filter((m) => !chosen.includes(m));
      layer++;
    }
    return result;
  }, [rangeMarkers]);

  if (!duration) return null;

  return (
    <>
      {dotMarkers.map((m, i) => {
        const leftPct = (m.seconds / duration) * 100;
        const color = tagColors[m.primaryTag.name];
        return (
          <Tooltip key={`dot-${i}`} trackCursorAxis="x">
            <TooltipTrigger
              delay={0}
              closeDelay={0}
              render={
                <div
                  className="player-marker-dot"
                  style={{
                    left: `calc(${leftPct}% - ${DOT_R}px)`,
                    ...(color ? { backgroundColor: color } : {}),
                  }}
                />
              }
            />
            <TooltipContent>{m.title}</TooltipContent>
          </Tooltip>
        );
      })}

      {layeredRanges.map(({ marker: m, layer }, i) => {
        const startPct = (m.seconds / duration) * 100;
        const widthPct = (((m.end_seconds ?? 0) - m.seconds) / duration) * 100;
        const color = tagColors[m.primaryTag.name];
        return (
          <Tooltip key={`range-${i}`} trackCursorAxis="x">
            <TooltipTrigger
              delay={0}
              closeDelay={0}
              render={
                <div
                  className="player-marker-range"
                  style={{
                    left: `${startPct}%`,
                    width: `${widthPct}%`,
                    bottom: `${layer * LAYER_HEIGHT_PX}px`,
                    ...(color ? { backgroundColor: color } : {}),
                  }}
                />
              }
            />
            <TooltipContent>{m.title}</TooltipContent>
          </Tooltip>
        );
      })}
    </>
  );
}
