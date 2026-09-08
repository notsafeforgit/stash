import {
  automaticMigrationEnabled,
  commitImportedEntry,
  getEntry,
  hasImportReceipt,
  readOfflineSource,
  readOfflineSourceEntry,
  type OfflineEntry,
} from "./offline-db";
import {
  createOfflineScope,
  deploymentFromDatabase,
  getOfflineScope,
  LEGACY_OFFLINE_DATABASE,
  type OfflineScope,
} from "./offline-scope";
import {
  opfsPathForScene,
  readSceneFromScope,
  removeScene,
  writeScene,
} from "./opfs-storage";
import {
  legacyEntryDeployment,
  relocateEntryPaths,
  sameDownload,
} from "./offline-migration-policy";

export interface RecoverySource {
  databaseName: string;
  deploymentURL?: string;
  entries: OfflineEntry[];
  invalid: number;
}
export interface MigrationProgress {
  running: boolean;
  completed: number;
  total: number;
  error?: Error;
}
let progress: MigrationProgress = { running: false, completed: 0, total: 0 };
const listeners = new Set<() => void>();
export const getMigrationProgress = () => progress;
export function subscribeToMigration(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
function publish(next: MigrationProgress) {
  progress = next;
  for (const listener of listeners) listener();
}

export async function listRecoverySources(): Promise<RecoverySource[]> {
  const current = await getOfflineScope();
  const names =
    typeof indexedDB.databases === "function"
      ? (await indexedDB.databases()).flatMap(({ name }) =>
          name ? [name] : [],
        )
      : [LEGACY_OFFLINE_DATABASE];
  const sources: RecoverySource[] = [];
  for (const databaseName of names.sort()) {
    if (databaseName === current.databaseName) continue;
    const deploymentURL = deploymentFromDatabase(databaseName);
    if (databaseName !== LEGACY_OFFLINE_DATABASE && !deploymentURL) continue;
    const source = await readOfflineSource(databaseName);
    if (source.entries.length || source.invalid)
      sources.push({ databaseName, deploymentURL, ...source });
  }
  return sources;
}

/** Also supports browsers without database enumeration. Inspection performs
 * no network request and never creates or alters a source database. */
export async function inspectRecoverySource(
  address: string,
): Promise<RecoverySource> {
  const scope = await createOfflineScope(address);
  await sourceScope(scope.databaseName);
  return {
    databaseName: scope.databaseName,
    deploymentURL: scope.deploymentURL,
    ...(await readOfflineSource(scope.databaseName)),
  };
}

export class OfflineSourceBusyError extends Error {
  constructor() {
    super(
      "The saved downloads are in use in another tab. Close that tab and retry.",
    );
  }
}

async function sourceScope(databaseName: string): Promise<OfflineScope | null> {
  if (databaseName === LEGACY_OFFLINE_DATABASE) return null;
  const deployment = deploymentFromDatabase(databaseName);
  if (!deployment) throw new Error("Unknown offline storage source");
  const scope = await createOfflineScope(deployment);
  if (scope.databaseName === (await getOfflineScope()).databaseName)
    throw new Error("Cannot restore a library into itself");
  return scope;
}

/** Destination worker ownership is always acquired first; source locks are
 * non-blocking, so opposing imports cannot deadlock. Scene commands also hold
 * scene locks, even when the source's worker is idle. */
async function importEntries(
  source: RecoverySource,
  ids: ReadonlySet<string>,
  automatic: boolean,
  signal: AbortSignal,
) {
  const destination = await getOfflineScope();
  const origin = await sourceScope(source.databaseName);
  return navigator.locks.request(
    origin?.workerLock ?? "stash-offline-worker",
    { ifAvailable: true },
    async (lock) => {
      if (!lock) throw new OfflineSourceBusyError();
      const fresh = await readOfflineSource(source.databaseName);
      const selected = fresh.entries.filter((entry) => ids.has(entry.scene_id));
      publish({ running: true, completed: 0, total: selected.length });
      let imported = 0;
      for (const entry of selected) {
        signal.throwIfAborted();
        // Recheck ownership under the source lock, not just in the UI's snapshot.
        if (
          automatic &&
          (entry.status === "downloading" ||
            legacyEntryDeployment(entry) !== destination.deploymentURL)
        )
          continue;
        const sourceLock =
          origin?.sceneLock(entry.scene_id) ??
          `stash-offline-scene:${entry.scene_id}`;
        await navigator.locks.request(
          sourceLock,
          { ifAvailable: true },
          async (lock) => {
            if (!lock) throw new OfflineSourceBusyError();
            await navigator.locks.request(
              destination.sceneLock(entry.scene_id),
              { signal },
              async () => {
                if (
                  automatic &&
                  (await hasImportReceipt(source.databaseName, entry.scene_id))
                )
                  return;
                const existing = await getEntry(entry.scene_id);
                if (existing) {
                  await commitImportedEntry(source.databaseName, existing);
                  return;
                }
                let fileWritten = false;
                try {
                  const file = await readSceneFromScope(entry.scene_id, origin);
                  if (file) {
                    fileWritten = true;
                    const bytes = await writeScene(
                      entry.scene_id,
                      file.stream(),
                      signal,
                    );
                    if (bytes !== file.size)
                      throw new Error(
                        "The restored file size does not match its source",
                      );
                  }
                  signal.throwIfAborted();
                  // Older app versions do not participate in Web Locks. Recheck both
                  // metadata and the source file snapshot before publishing the copy.
                  const latest = await readOfflineSourceEntry(
                    source.databaseName,
                    entry.scene_id,
                  );
                  const latestFile = await readSceneFromScope(
                    entry.scene_id,
                    origin,
                  );
                  if (
                    !latest ||
                    !sameDownload(entry, latest) ||
                    latestFile?.size !== file?.size ||
                    latestFile?.lastModified !== file?.lastModified
                  )
                    throw new Error(
                      "The saved download changed while restoring. Close other tabs and retry.",
                    );
                  const complete =
                    entry.status === "complete" &&
                    file !== null &&
                    file.size === entry.bytes;
                  await commitImportedEntry(source.databaseName, {
                    ...latest,
                    paths: relocateEntryPaths(
                      latest,
                      origin?.deploymentURL ?? legacyEntryDeployment(latest),
                      destination.deploymentURL,
                    ),
                    opfs_path: await opfsPathForScene(entry.scene_id),
                    status: complete ? "complete" : "error",
                    error: complete
                      ? undefined
                      : "The saved download is incomplete. Retry to continue.",
                    request_id: undefined,
                    queued_at: undefined,
                    cancel_requested: false,
                    bytes_downloaded: undefined,
                  });
                  imported++;
                } catch (error) {
                  // A receipt only commits alongside metadata. Failed copies remain
                  // retryable and never remove either the source or an existing row.
                  if (fileWritten && !(await getEntry(entry.scene_id)))
                    await removeScene(entry.scene_id);
                  throw error;
                }
              },
            );
          },
        );
        publish({ ...progress, completed: progress.completed + 1 });
      }
      return imported;
    },
  );
}

function recordFailure(error: unknown) {
  publish({
    ...progress,
    running: false,
    error:
      error instanceof DOMException && error.name === "AbortError"
        ? undefined
        : error instanceof Error
          ? error
          : new Error(String(error)),
  });
}

let attemptedAutomaticMigration = false;
let automaticAbort: AbortController | undefined;
export function cancelAutomaticMigration() {
  automaticAbort?.abort();
}
/** Called under this deployment's worker lock. Only known owners migrate
 * automatically. Import receipts survive deletion and clear-all, preventing
 * legacy backups from resurrecting downloads. No source is ever changed. */
export async function migrateLegacyDownloads(): Promise<void> {
  if (attemptedAutomaticMigration) return;
  attemptedAutomaticMigration = true;
  try {
    if (!(await automaticMigrationEnabled())) return;
    const current = await getOfflineScope();
    const legacy = await readOfflineSource(LEGACY_OFFLINE_DATABASE);
    const entries: OfflineEntry[] = [];
    for (const entry of legacy.entries) {
      if (
        entry.status !== "downloading" &&
        legacyEntryDeployment(entry) === current.deploymentURL &&
        !(await hasImportReceipt(LEGACY_OFFLINE_DATABASE, entry.scene_id))
      )
        entries.push(entry);
    }
    if (!entries.length) return;
    automaticAbort = new AbortController();
    await importEntries(
      { databaseName: LEGACY_OFFLINE_DATABASE, ...legacy },
      new Set(entries.map((entry) => entry.scene_id)),
      true,
      automaticAbort.signal,
    );
    publish({ ...progress, running: false });
  } catch (error) {
    recordFailure(error);
  } finally {
    automaticAbort = undefined;
  }
}

/** Explicit recovery can copy ambiguous legacy entries or a previous prefix.
 * Callers present the source and selected scenes for ownership confirmation. */
export async function restoreDownloads(
  source: RecoverySource,
  ids: ReadonlySet<string>,
  signal: AbortSignal,
): Promise<number> {
  if (!navigator.locks || typeof BroadcastChannel === "undefined")
    throw new Error(
      "Restoring downloads requires browser storage coordination",
    );
  try {
    const scope = await getOfflineScope();
    const count = await navigator.locks.request(
      scope.workerLock,
      { signal },
      () => importEntries(source, ids, false, signal),
    );
    publish({ ...progress, running: false });
    return count;
  } catch (error) {
    recordFailure(error);
    throw error;
  }
}
