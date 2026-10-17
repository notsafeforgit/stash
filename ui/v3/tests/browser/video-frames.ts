import type { Page } from "@playwright/test";

/** Observe compositor frames from both playback and lazily created previews. */
export async function observeVideoFrames(page: Page) {
  await page.addInitScript(() => {
    const observed = new WeakSet<HTMLVideoElement>();
    const watch = (video: HTMLVideoElement) => {
      if (observed.has(video)) return;
      observed.add(video);
      const observe = (_now: number, frame: VideoFrameCallbackMetadata) => {
        video.dataset.decodedTime = String(frame.mediaTime);
        video.requestVideoFrameCallback(observe);
      };
      video.requestVideoFrameCallback(observe);
    };
    new MutationObserver((records) => {
      for (const record of records)
        for (const node of record.addedNodes) {
          if (node instanceof HTMLVideoElement) watch(node);
          else if (node instanceof Element)
            node.querySelectorAll("video").forEach(watch);
        }
    }).observe(document, { childList: true, subtree: true });
  });
}
