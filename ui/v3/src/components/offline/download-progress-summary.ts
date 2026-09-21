import type { IntlShape } from "react-intl";
import {
  downloadProgressValue,
  type DownloadProgressInput,
} from "./download-processing";

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let n = bytes;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n < 10 && i > 0 ? n.toFixed(1) : Math.round(n)} ${units[i]}`;
}

export function downloadProgressSummary(
  intl: IntlShape,
  active: DownloadProgressInput,
): string {
  const progress = downloadProgressValue(active);
  const received = formatBytes(active.bytesDownloaded);
  if (progress.kind === "saving")
    return intl.formatMessage({ id: "offline.progress.saving" }, { received });
  if (progress.kind === "processing")
    return intl.formatMessage(
      {
        id:
          progress.percent === null
            ? "offline.progress.processing_unknown"
            : "offline.progress.processing",
      },
      { received, percent: progress.percent },
    );
  if (active.bytesTotal != null && active.bytesTotal > 0)
    return `${received} / ${formatBytes(active.bytesTotal)} · ${progress.percent}%`;
  return received;
}
