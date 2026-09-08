import type { OfflineEntry } from "./offline-db";
import { normalizeDeploymentURL } from "./offline-scope";

const ARTWORK_PATHS = ["screenshot", "preview", "sprite", "vtt"] as const;

function artworkDeployment(
  kind: keyof OfflineEntry["paths"],
  value: string,
  sceneId: string,
): string | undefined {
  if (!value.startsWith("/") && !/^https?:\/\//i.test(value)) return undefined;
  try {
    const url = new URL(value, window.location.origin);
    const separator = url.pathname.lastIndexOf("/scene/");
    if (separator < 0) return undefined;
    const tail = url.pathname.slice(separator + "/scene/".length);
    const recognized =
      (kind === "screenshot" && tail === `${sceneId}/screenshot`) ||
      (kind === "preview" && tail === `${sceneId}/preview`) ||
      (kind === "sprite" && /^[^/]+_sprite\.jpg$/.test(tail)) ||
      (kind === "vtt" && /^[^/]+_thumbs\.vtt$/.test(tail));
    if (!recognized) return undefined;
    return normalizeDeploymentURL(
      url.origin + url.pathname.slice(0, separator),
    );
  } catch {
    return undefined;
  }
}

/** Old entries lack an installation ID. Only recognized server-generated
 * artwork paths that all agree supply ownership evidence; scene IDs alone do not. */
export function legacyEntryDeployment(entry: OfflineEntry): string | undefined {
  let deployment: string | undefined;
  for (const kind of ARTWORK_PATHS) {
    const value = entry.paths[kind];
    if (!value) continue;
    const candidate = artworkDeployment(kind, value, entry.scene_id);
    if (!candidate || (deployment && deployment !== candidate))
      return undefined;
    deployment = candidate;
  }
  return deployment;
}

export function sameDownload(a: OfflineEntry, b: OfflineEntry): boolean {
  return (
    a.scene_id === b.scene_id &&
    a.request_id === b.request_id &&
    a.downloaded_at === b.downloaded_at &&
    a.bytes === b.bytes &&
    a.status === b.status &&
    a.format === b.format &&
    a.resolution === b.resolution &&
    a.source_file_path === b.source_file_path &&
    a.paths.screenshot === b.paths.screenshot &&
    a.paths.preview === b.paths.preview &&
    a.paths.sprite === b.paths.sprite &&
    a.paths.vtt === b.paths.vtt
  );
}

/** After an explicit address-change recovery, recognized artwork can follow
 * the new prefix. Unknown/CDN paths stay unchanged. Queries retain signatures. */
export function relocateEntryPaths(
  entry: OfflineEntry,
  from: string | undefined,
  to: string,
): OfflineEntry["paths"] {
  const relocate = (kind: keyof OfflineEntry["paths"]) => {
    const value = entry.paths[kind];
    if (
      !value ||
      !from ||
      artworkDeployment(kind, value, entry.scene_id) !== from
    )
      return value;
    try {
      const url = new URL(value, window.location.origin);
      const source = new URL(from);
      return new URL(
        url.pathname.slice(source.pathname.length) + url.search + url.hash,
        to,
      ).href;
    } catch {
      return value;
    }
  };
  return {
    ...entry.paths,
    screenshot: relocate("screenshot"),
    preview: relocate("preview"),
    sprite: relocate("sprite"),
    vtt: relocate("vtt"),
  };
}
