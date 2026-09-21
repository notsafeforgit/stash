/**
 * Thin global download-progress strip — mounted at the app shell so
 * users see download activity from any route. Renders nothing when
 * the queue is idle so it costs no visual real estate the rest of
 * the time.
 *
 * Known byte totals use transfer progress; streaming transcodes/remuxes
 * use processed video time. Without either total, the bar is indeterminate.
 * Progress stays below 100% until the local file has finished saving.
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
import { downloadProgressValue } from "./download-processing";
import { downloadProgressSummary } from "./download-progress-summary";
import "./download-progress-bar.css";

export function DownloadProgressBar() {
  const intl = useIntl();
  const queue = useDownloadQueue();
  const active = queue.state.active;
  if (!active) return null;

  const { percent } = downloadProgressValue(active);

  return (
    <Progress
      value={percent}
      aria-valuetext={downloadProgressSummary(intl, active)}
      aria-label={intl.formatMessage({
        id: "offline.notifications.progress_aria",
      })}
      className="block w-full"
    >
      <ProgressTrack className="relative block h-0.5 w-full overflow-hidden bg-primary/10">
        <ProgressIndicator
          className={cn(
            "h-full bg-primary",
            percent === null && "download-progress-indeterminate",
          )}
        />
      </ProgressTrack>
    </Progress>
  );
}
