import { useState } from "react";
import { useTaskDefaults, type ITaskDefaults } from "./use-task-defaults";

/**
 * Typed [opts, setOpts] for one key under `ui.taskDefaults`. The setter
 * updates local state immediately and debounce-persists through
 * `useTaskDefaults.save`.
 *
 * Initial value: `taskDefaults[key]` if present, otherwise `fallback()`.
 * Lazy fallback so callers can derive from `configuration.defaults.X` (and
 * `withoutTypename` it) without paying the cost on every render.
 *
 * @example
 * const [scanOptions, setScanOptions] = useTaskOptions("scan", () =>
 *   configuration.defaults.scan
 *     ? withoutTypename(configuration.defaults.scan)
 *     : { scanGenerateCovers: true },
 * );
 */
export function useTaskOptions<K extends keyof ITaskDefaults>(
  key: K,
  fallback: () => NonNullable<ITaskDefaults[K]>,
): [
  NonNullable<ITaskDefaults[K]>,
  (value: NonNullable<ITaskDefaults[K]>) => void,
] {
  const { taskDefaults, save } = useTaskDefaults();
  const [value, setValue] = useState<NonNullable<ITaskDefaults[K]>>(
    () => (taskDefaults[key] as NonNullable<ITaskDefaults[K]>) ?? fallback(),
  );

  function setAndPersist(v: NonNullable<ITaskDefaults[K]>) {
    setValue(v);
    save(key, v);
  }

  return [value, setAndPersist];
}
