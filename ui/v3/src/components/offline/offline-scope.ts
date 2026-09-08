import { getPlatformURL } from "@/core/platform-url";

export const LEGACY_OFFLINE_DATABASE = "stash-offline";
export const OFFLINE_DATABASE_PREFIX = "stash-offline:v1:";

export function canCoordinateOfflineStorage(): boolean {
  return (
    typeof navigator !== "undefined" &&
    !!navigator.locks &&
    typeof BroadcastChannel !== "undefined"
  );
}

/** The backend mount point identifies an installation, independently of v3
 * routes. Credentials, queries, fragments and insignificant trailing slashes
 * never become persistent identifiers. The browser already separates origins. */
export function normalizeDeploymentURL(value: string | URL): string {
  const url = new URL(value);
  if (url.protocol !== "http:" && url.protocol !== "https:")
    throw new Error("Offline storage requires an HTTP(S) deployment URL");
  return `${url.origin}${url.pathname.replace(/\/+$/, "")}/`;
}

export interface OfflineScope {
  readonly deploymentURL: string;
  readonly databaseName: string;
  readonly directory: string;
  readonly channelName: string;
  readonly workerLock: string;
  sceneLock(sceneId: string): string;
}

export async function createOfflineScope(
  base: string | URL,
): Promise<OfflineScope> {
  const deploymentURL = normalizeDeploymentURL(base);
  // A fixed-size directory segment avoids filesystem filename limits for long
  // or Unicode public prefixes. The database name retains the recoverable URL.
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(deploymentURL),
  );
  const directory = Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
  const databaseName = `${OFFLINE_DATABASE_PREFIX}${deploymentURL}`;
  return {
    deploymentURL,
    databaseName,
    directory,
    channelName: databaseName,
    workerLock: `${databaseName}:worker`,
    sceneLock: (sceneId) => `${databaseName}:scene:${sceneId}`,
  };
}

let current: Promise<OfflineScope> | undefined;
export function getOfflineScope(): Promise<OfflineScope> {
  current ??= createOfflineScope(getPlatformURL()).catch((error: unknown) => {
    current = undefined;
    throw error;
  });
  return current;
}

export function deploymentFromDatabase(name: string): string | undefined {
  if (!name.startsWith(OFFLINE_DATABASE_PREFIX)) return undefined;
  const value = name.slice(OFFLINE_DATABASE_PREFIX.length);
  try {
    return normalizeDeploymentURL(value) === value ? value : undefined;
  } catch {
    return undefined;
  }
}

export function sceneFilename(sceneId: string): string {
  if (!/^[1-9]\d*$/.test(sceneId)) throw new Error("Invalid offline scene ID");
  return `${sceneId}.mp4`;
}
