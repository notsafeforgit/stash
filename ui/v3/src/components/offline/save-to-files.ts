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
 *      the OPFS file and triggers an anchor click. On iOS this
 *      surfaces the system share sheet, which lets the user save to
 *      Files / send via AirDrop / etc.
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
  const file = await readScene(entry.scene_id);
  if (!file) throw new FileMissingError(entry.scene_id);

  const suggested = ensureMp4Extension(filenameStemForEntry(entry));

  if (
    "showSaveFilePicker" in window &&
    typeof window.showSaveFilePicker === "function"
  ) {
    await saveViaFSA(file, suggested);
    return;
  }

  saveViaAnchor(file, suggested);
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

async function saveViaFSA(file: File, suggested: string): Promise<void> {
  const picker = window.showSaveFilePicker;
  if (!picker) throw new Error("showSaveFilePicker not available");
  const handle = await picker({
    suggestedName: suggested,
    types: [
      {
        description: "MP4 Video",
        accept: { "video/mp4": [".mp4"] },
      },
    ],
  });
  const writable = await handle.createWritable();
  // file.stream() returns a ReadableStream<Uint8Array>; pipeTo handles
  // backpressure so multi-GB writes don't balloon the JS heap.
  await file.stream().pipeTo(writable);
}

function saveViaAnchor(file: File, suggested: string): void {
  const url = URL.createObjectURL(file);
  const a = document.createElement("a");
  a.href = url;
  a.download = suggested;
  document.body.append(a);
  a.click();
  a.remove();
  // Defer revoke a tick so the browser actually consumes the URL.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

function ensureMp4Extension(name: string): string {
  if (/\.mp4$/i.test(name)) return name;
  return `${name}.mp4`;
}
