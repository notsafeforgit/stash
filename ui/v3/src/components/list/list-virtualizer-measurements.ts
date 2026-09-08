import type { VirtualItem } from "@tanstack/react-virtual";

export interface ListMeasurements {
  width: number;
  itemIds: readonly string[];
  rows: VirtualItem[];
}

export function matchesListMeasurements(
  saved: ListMeasurements,
  width: number,
  itemIds?: readonly string[],
): boolean {
  return (
    saved.width === width &&
    (!itemIds ||
      (saved.itemIds.length === itemIds.length &&
        saved.itemIds.every((id, index) => id === itemIds[index])))
  );
}

/** Geometry only; scroll offsets remain owned by TanStack Router. Keep a
 * bounded set of recent list layouts instead of retaining mounted cards. */
export class ListMeasurementsCache {
  private readonly entries = new Map<string, ListMeasurements>();

  constructor(private readonly limit = 20) {}

  get(key: string): ListMeasurements | undefined {
    return this.entries.get(key);
  }

  set(key: string, measurements: ListMeasurements): void {
    this.entries.delete(key);
    this.entries.set(key, measurements);
    if (this.entries.size > this.limit) {
      const oldest = this.entries.keys().next().value;
      if (oldest !== undefined) this.entries.delete(oldest);
    }
  }
}

export const listMeasurementsCache = new ListMeasurementsCache();
