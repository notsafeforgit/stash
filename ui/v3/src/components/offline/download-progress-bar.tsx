/**
 * Thin global download-progress strip — mounted at the app shell so
 * users see download activity from any route. Renders nothing when
 * the queue is idle so it costs no visual real estate the rest of
 * the time.
 *
 * Determinate vs indeterminate: when `bytesTotal` is known (Content-
 * Length header from the server) we fill proportionally; when not (a
 * live-transcoded stream has no length up-front) we show an
 * indeterminate sliding bar.
 *
 * Position: a 2 px strip glued to the very top of the page main area
 * (just under the header). Visible on both desktop and mobile because
 * the header is on every page; survives narrow viewports because it
 * spans 100% width with no min-width.
 */

import { useIntl } from "react-intl";
import {
  Progress,
  ProgressTrack,
  ProgressIndicator,
} from "src/components/ui/progress";
import { cn } from "src/lib/utils";
import { useDownloadQueue } from "./use-download-queue";
import "./download-progress-bar.css";

export function DownloadProgressBar() {
  const intl = useIntl();
  const queue = useDownloadQueue();
  const active = queue.state.active;
  if (!active) return null;

  const determinate = active.bytesTotal != null && active.bytesTotal > 0;
  const pct = determinate
    ? Math.min(
        100,
        Math.round((active.bytesDownloaded / active.bytesTotal!) * 100),
      )
    : 0;

  return (
    <Progress
      value={determinate ? pct : null}
      aria-label={intl.formatMessage({
        id: "offline.notifications.progress_aria",
      })}
      className="block w-full"
    >
      <ProgressTrack className="relative block h-0.5 w-full overflow-hidden bg-primary/10">
        <ProgressIndicator
          className={cn(
            "h-full bg-primary",
            !determinate && "download-progress-indeterminate",
          )}
        />
      </ProgressTrack>
    </Progress>
  );
}
