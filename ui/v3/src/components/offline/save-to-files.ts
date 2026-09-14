/**
 * "Save to Files" — export a downloaded scene out of OPFS into the
 * user's regular device storage.
 *
 * Two-tier strategy:
 *
 *   1. File System Access API (`window.showSaveFilePicker`). Available
 *      on desktop Chrome / Edge / Opera. Pops a real save dialog,
 *      streams OPFS → chosen location with no extra in-memory copy.
 *
 *   2. Anchor download fallback. Available on Safari (incl. iOS),
 *      Firefox, anywhere without FSA. Creates a `blob:` URL backed by
 *      the OPFS file and triggers an anchor click. The browser owns
 *      the download UI and destination; this does not use native sharing.
 *
 * Both paths feature-detect; no UA sniffing.
 */

import { entryDisplayTitle, type OfflineEntry } from "./offline-db";
import { readScene } from "./opfs-storage";

// File System Access API isn't in the TS DOM lib yet (Chrome / Edge /
// Opera ship it; standardisation in progress). Narrow declarations
// covering only what we use, gated by feature detection at the call
// site so non-supporting browsers fall through harmlessly.
interface FsaFilePickerOptions {
  suggestedName?: string;
  types?: {
    description?: string;
    accept: Record<string, string[]>;
  }[];
}
interface FsaFileHandle {
  createWritable(): Promise<WritableStream<Uint8Array>>;
}
declare global {
  interface Window {
    showSaveFilePicker?: (
      options?: FsaFilePickerOptions,
    ) => Promise<FsaFileHandle>;
  }
}

export class FileMissingError extends Error {
  constructor(sceneId: string) {
    super(`OPFS entry missing for scene ${sceneId}`);
    this.name = "FileMissingError";
  }
}

/**
 * Save a downloaded scene to the user's regular device storage. The
 * filename is derived from the entry — title preferred, source filename
 * as fallback, scene id only as a last resort — so the user gets a
 * recognisable name in their Files / Downloads folder rather than a
 * bare numeric id. Sanitised for cross-OS filesystem safety
 * (Windows + macOS + Linux + iOS) before being handed to the picker /
 * anchor.
 */
export async function saveToFiles(entry: OfflineEntry): Promise<void> {
  const suggested = ensureMp4Extension(filenameStemForEntry(entry));
  let destination: FsaFileHandle | undefined;

  if (typeof window.showSaveFilePicker === "function") {
    try {
      // Open during the user's gesture, before slow OPFS/IndexedDB reads can
      // expire transient activation. Cancelling the picker is a normal exit.
      destination = await window.showSaveFilePicker({
        suggestedName: suggested,
        types: [
          { description: "MP4 Video", accept: { "video/mp4": [".mp4"] } },
        ],
      });
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") return;
      throw error;
    }
  }

  const file = await readScene(entry.scene_id);
  if (!file) throw new FileMissingError(entry.scene_id);
  if (destination) {
    const writable = await destination.createWritable();
    // Stream with backpressure; multi-GB exports need no extra in-memory copy.
    await file.stream().pipeTo(writable);
  } else {
    saveViaAnchor(file, suggested);
  }
}

function filenameStemForEntry(entry: OfflineEntry): string {
  const raw = entryDisplayTitle(entry);
  return sanitiseFilename(raw) || entry.scene_id;
}

// Reserved filesystem chars + ASCII control range (U+0000–U+001F).
// The control range is built via String.fromCharCode so the regex source
// doesn't contain literal control characters (eslint no-control-regex).
const FORBIDDEN_FILENAME_CHARS = new RegExp(
  `[\\\\/:*?"<>|${String.fromCharCode(0)}-${String.fromCharCode(31)}]`,
  "g",
);

/**
 * Strip characters that Windows / macOS / iOS Files reject in
 * filenames, collapse runs of whitespace, and trim leading dots
 * (hidden-file convention on POSIX, plus iOS quirks). The browsers'
 * own download codepath does some of this for us, but doing it
 * up-front means the FSA picker gets a name it can use as-is.
 */
function sanitiseFilename(name: string): string {
  return name
    .replace(FORBIDDEN_FILENAME_CHARS, " ")
    .replace(/\s+/g, " ")
    .replace(/^\.+/, "")
    .trim();
}

function saveViaAnchor(file: File, suggested: string): void {
  const url = URL.createObjectURL(file);
  const a = document.createElement("a");
  a.href = url;
  a.download = suggested;
  document.body.append(a);
  a.click();
  a.remove();
  // Safari may consume a download URL after the click task has finished. Keep
  // the disk-backed reference briefly, then release it without retaining it
  // for the lifetime of this tab.
  setTimeout(() => URL.revokeObjectURL(url), 30_000);
}

function ensureMp4Extension(name: string): string {
  if (/\.mp4$/i.test(name)) return name;
  return `${name}.mp4`;
}
