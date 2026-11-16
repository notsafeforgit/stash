import { useState, useSyncExternalStore } from "react";
import { useIntl } from "react-intl";
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  SCRUB_TIMING_TIERS,
  type ScrubTimingTier,
  scrubDebugEnabled,
  scrubTimingSnapshot,
  subscribeScrubTimings,
} from "./scrub-timings";

/** Opt-in device diagnostics, enabled by `localStorage["stash.scrubDebug"]`
 * set to "1". When off, nothing subscribes to the timings or renders. */
export function ScrubTimingReadout() {
  const [enabled] = useState(scrubDebugEnabled);
  return enabled ? <ScrubTimingTable /> : null;
}

function ScrubTimingTable() {
  const intl = useIntl();
  const summary = useSyncExternalStore(
    subscribeScrubTimings,
    scrubTimingSnapshot,
    scrubTimingSnapshot,
  );
  const labels: Record<ScrubTimingTier, string> = {
    "still-cache": intl.formatMessage({
      id: "media_player.scrub_timings.still_cache",
      defaultMessage: "Cached still",
    }),
    "still-network": intl.formatMessage({
      id: "media_player.scrub_timings.still_network",
      defaultMessage: "Fetched still",
    }),
    "exact-frame": intl.formatMessage({
      id: "media_player.scrub_timings.exact_frame",
      defaultMessage: "Exact frame",
    }),
    "main-buffer": intl.formatMessage({
      id: "media_player.scrub_timings.main_buffer",
      defaultMessage: "Buffered seek",
    }),
  };
  const duration = (ms: number | null) =>
    ms === null
      ? "–"
      : intl.formatNumber(ms, {
          style: "unit",
          unit: "millisecond",
          unitDisplay: "narrow",
          maximumFractionDigits: 0,
        });
  return (
    // A passive overlay: player gestures and controls beneath it keep working.
    // `window.__stashScrubTimings` exposes the same values for copying.
    <div
      data-scrub-timings
      className="pointer-events-none absolute top-[calc(var(--safe-area-top)+0.5rem)] left-[calc(var(--safe-area-left)+0.5rem)] z-20 rounded-md bg-black/60 text-white text-xs"
    >
      <Table className="text-xs">
        <TableCaption className="mt-0 px-2 pt-1 text-left text-white/80">
          {intl.formatMessage({
            id: "media_player.scrub_timings.title",
            defaultMessage: "Scrub preview timings",
          })}
        </TableCaption>
        <TableHeader>
          <TableRow className="border-white/20 hover:bg-transparent">
            <TableHead className="h-6 px-2 text-white/80">
              {intl.formatMessage({
                id: "media_player.scrub_timings.tier",
                defaultMessage: "Tier",
              })}
            </TableHead>
            <TableHead className="h-6 px-2 text-right text-white/80">
              {intl.formatMessage({
                id: "media_player.scrub_timings.count",
                defaultMessage: "Count",
              })}
            </TableHead>
            <TableHead className="h-6 px-2 text-right text-white/80">
              {intl.formatMessage({
                id: "media_player.scrub_timings.median",
                defaultMessage: "Median",
              })}
            </TableHead>
            <TableHead className="h-6 px-2 text-right text-white/80">
              {intl.formatMessage({
                id: "media_player.scrub_timings.p90",
                defaultMessage: "90th percentile",
              })}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {SCRUB_TIMING_TIERS.map((tier) => (
            <TableRow
              key={tier}
              data-scrub-timing-tier={tier}
              className="border-white/20 hover:bg-transparent"
            >
              <TableCell className="px-2 py-1">{labels[tier]}</TableCell>
              <TableCell
                className="px-2 py-1 text-right tabular-nums"
                data-selectable-text
              >
                {intl.formatNumber(summary[tier].count)}
              </TableCell>
              <TableCell
                className="px-2 py-1 text-right tabular-nums"
                data-selectable-text
              >
                {duration(summary[tier].median)}
              </TableCell>
              <TableCell
                className="px-2 py-1 text-right tabular-nums"
                data-selectable-text
              >
                {duration(summary[tier].p90)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
