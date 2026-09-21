import { z } from "zod";
import { joinPlatformURL } from "@/core/platform-url";
import { getOfflineScope } from "./offline-scope";
import type { OfflineEntry } from "./offline-db";

const processingSchema = z.object({
  request_id: z.string(),
  state: z.enum(["processing", "finished", "failed"]),
  processed_seconds: z.number().nonnegative(),
  duration_seconds: z.number().nonnegative(),
});
export type DownloadProcessing = z.infer<typeof processingSchema>;
type DownloadAttempt = Pick<OfflineEntry, "scene_id" | "request_id">;
type ProgressResponse =
  | { available: false }
  | { available: true; progress: DownloadProcessing | null };

export const VIDEO_PROCESSING_FAILED = "Video processing failed";

/** Polling is optional telemetry. Older servers, missing records and temporary
 * network failures must not prevent the actual video transfer. */
async function readProcessing(
  entry: DownloadAttempt,
  signal?: AbortSignal,
): Promise<ProgressResponse> {
  const url = joinPlatformURL(
    (await getOfflineScope()).deploymentURL,
    `scene/${entry.scene_id}/download/progress`,
  );
  if (!entry.request_id) return { available: false };
  url.searchParams.set("request_id", entry.request_id);
  const request = new AbortController();
  const abort = () => request.abort();
  signal?.throwIfAborted();
  signal?.addEventListener("abort", abort, { once: true });
  const timeout = setTimeout(abort, 5000);
  try {
    const response = await fetch(url, {
      signal: request.signal,
      credentials: "include",
      cache: "no-store",
    });
    if (response.status === 204) return { available: true, progress: null };
    if ([401, 403, 404, 405, 501].includes(response.status))
      return { available: false };
    if (!response.ok) throw new Error(`Progress HTTP ${response.status}`);
    const body: unknown = await response.json().catch(() => null);
    const parsed = processingSchema.safeParse(body);
    if (!parsed.success || parsed.data.request_id !== entry.request_id)
      return { available: false };
    return { available: true, progress: parsed.data };
  } finally {
    clearTimeout(timeout);
    signal?.removeEventListener("abort", abort);
  }
}

/** One non-overlapping poll per active page. Each attempt gets a fresh watcher;
 * stopping it aborts in-flight requests and suppresses their late responses. */
export function watchDownloadProcessing(
  entry: DownloadAttempt,
  onProgress: (progress: DownloadProcessing) => void,
): () => void {
  const abort = new AbortController();
  let timer: ReturnType<typeof setTimeout> | undefined;
  let delay = 1000;
  const poll = async () => {
    try {
      const result = await readProcessing(entry, abort.signal);
      if (abort.signal.aborted || !result.available) return;
      if (result.progress) {
        onProgress(result.progress);
        if (result.progress.state !== "processing") return;
        delay = 1000;
      } else {
        delay = Math.min(5000, delay + 1000);
      }
    } catch {
      delay = Math.min(15000, delay * 2);
    }
    if (!abort.signal.aborted) timer = setTimeout(() => void poll(), delay);
  };
  void poll();
  return () => {
    abort.abort();
    clearTimeout(timer);
  };
}

/** A streamed HTTP body can end normally even when FFmpeg failed. Check its
 * terminal result after EOF, while remaining compatible with older servers. */
export async function checkDownloadProcessing(
  entry: DownloadAttempt,
  signal?: AbortSignal,
) {
  let result: ProgressResponse;
  try {
    result = await readProcessing(entry, signal);
  } catch {
    signal?.throwIfAborted();
    return;
  }
  signal?.throwIfAborted();
  if (result.available && result.progress?.state === "failed")
    throw new Error(VIDEO_PROCESSING_FAILED);
}

export interface DownloadProgressInput {
  bytesDownloaded: number;
  bytesTotal: number | null;
  processing?: DownloadProcessing;
}

export function downloadProgressValue(active: DownloadProgressInput): {
  kind: "transfer" | "processing" | "saving";
  percent: number | null;
} {
  if (active.bytesTotal != null && active.bytesTotal > 0) {
    return {
      kind: active.bytesDownloaded >= active.bytesTotal ? "saving" : "transfer",
      percent: Math.min(
        99,
        Math.max(
          0,
          Math.floor((active.bytesDownloaded / active.bytesTotal) * 100),
        ),
      ),
    };
  }
  const processing = active.processing;
  if (processing?.state === "finished") return { kind: "saving", percent: 99 };
  if (processing?.state === "processing") {
    return {
      kind: "processing",
      percent:
        processing.duration_seconds > 0
          ? Math.min(
              99,
              Math.floor(
                (processing.processed_seconds / processing.duration_seconds) *
                  100,
              ),
            )
          : null,
    };
  }
  return { kind: "transfer", percent: null };
}
