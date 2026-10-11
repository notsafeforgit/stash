import { describe, expect, it, vi } from "vitest";
import { BulkDeleteError, deleteInBatches } from "./bulk-delete";

describe("bulk deletion", () => {
  it("reports only acknowledged batches and never overlaps requests", async () => {
    const ids = Array.from({ length: 120 }, (_, i) => String(i));
    let release: (() => void) | undefined;
    const destroy = vi.fn<(batch: string[]) => Promise<void>>(
      () =>
        new Promise<void>((resolve) => {
          release = resolve;
        }),
    );
    const progress = vi.fn();
    const run = deleteInBatches([...ids, "0"], destroy, progress);
    expect(destroy).toHaveBeenCalledTimes(1);
    expect(progress.mock.calls).toEqual([[{ completed: 0, total: 120 }]]);
    for (const completed of [50, 100, 120]) {
      release?.();
      await Promise.resolve();
      expect(progress).toHaveBeenLastCalledWith({ completed, total: 120 });
    }
    expect(await run).toEqual(ids);
    expect(destroy.mock.calls.map((call) => call[0])).toEqual([
      ids.slice(0, 50),
      ids.slice(50, 100),
      ids.slice(100),
    ]);
  });

  it("retains confirmed IDs and stops at an ambiguous failed request", async () => {
    const ids = Array.from({ length: 120 }, (_, i) => String(i));
    const failure = new Error("Response lost");
    const destroy = vi
      .fn<(batch: string[]) => Promise<void>>()
      .mockResolvedValueOnce(undefined)
      .mockRejectedValueOnce(failure);
    const progress = vi.fn();
    const error = await deleteInBatches(ids, destroy, progress).catch(
      (error: unknown) => error,
    );
    expect(error).toBeInstanceOf(BulkDeleteError);
    expect(error).toMatchObject({
      completedIds: ids.slice(0, 50),
      total: 120,
      cause: failure,
    });
    expect(destroy).toHaveBeenCalledTimes(2);
    expect(progress.mock.calls).toEqual([
      [{ completed: 0, total: 120 }],
      [{ completed: 50, total: 120 }],
    ]);
  });
});
