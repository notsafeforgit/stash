import { useMemo, useRef } from "react";
import type * as GQL from "src/core/generated-graphql";
import { useConfigurationContext, useConfigureUISetting } from "./config";
import { useDebounce } from "./debounce";

/**
 * Sub-keys we manage under `ui.taskDefaults`. Matches v2.5's layout so the
 * server-side config carries over seamlessly.
 */
export interface ITaskDefaults {
  scan?: GQL.ScanMetadataInput;
  autoTag?: GQL.AutoTagMetadataInput;
  generate?: GQL.GenerateMetadataInput;
  clean?: GQL.CleanMetadataInput;
  cleanGenerated?: GQL.CleanGeneratedInput;
}

type TaskKey = keyof ITaskDefaults;

/**
 * Reads/writes `ui.taskDefaults`. Saves are debounced 500ms (matches v2.5's
 * SettingsContext cadence) and routed through `configureUISetting`, which
 * also updates the Apollo cache so subsequent reads see the merge instantly.
 */
export function useTaskDefaults() {
  const { configuration } = useConfigurationContext();
  const [configureUISetting] = useConfigureUISetting();

  const taskDefaults =
    (configuration.ui as { taskDefaults?: ITaskDefaults }).taskDefaults ?? {};

  // Keep a live ref so the debounced save reads the latest merged record at
  // flush time rather than the snapshot at the moment it was scheduled.
  const taskDefaultsRef = useRef<ITaskDefaults>(taskDefaults);
  taskDefaultsRef.current = taskDefaults;

  const flush = useDebounce(async (next: ITaskDefaults) => {
    await configureUISetting({
      variables: { key: "taskDefaults", value: next },
    });
  }, 500);

  const save = useMemo(
    () =>
      function save<K extends TaskKey>(key: K, value: ITaskDefaults[K]) {
        const next = { ...taskDefaultsRef.current, [key]: value };
        taskDefaultsRef.current = next;
        flush(next);
      },
    [flush],
  );

  return { taskDefaults, save };
}
