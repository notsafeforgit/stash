export interface DeleteProgress {
  completed: number;
  total: number;
}

export class BulkDeleteError extends Error {
  constructor(
    readonly completedIds: string[],
    readonly total: number,
    cause: unknown,
  ) {
    super("Bulk deletion stopped", { cause });
  }
}

/** Bound each transaction and report only acknowledged commits. Never retry a
 * failed request automatically: its response may have been lost after commit. */
export async function deleteInBatches(
  ids: readonly string[],
  destroy: (batch: string[]) => Promise<void>,
  onProgress: (progress: DeleteProgress) => void,
): Promise<string[]> {
  const uniqueIds = [...new Set(ids)];
  const completed: string[] = [];
  const total = uniqueIds.length;
  onProgress({ completed: 0, total });
  try {
    for (let offset = 0; offset < total; offset += 50) {
      const batch = uniqueIds.slice(offset, offset + 50);
      await destroy(batch);
      completed.push(...batch);
      onProgress({ completed: completed.length, total });
    }
  } catch (cause) {
    throw new BulkDeleteError(completed, total, cause);
  }
  return completed;
}
