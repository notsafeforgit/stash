import { useEffect, useMemo, useRef } from "react";
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
type PendingTaskDefaultsSave = {
  id: number;
  defaults: ITaskDefaults;
};

let pendingTaskDefaults: ITaskDefaults | undefined;
let saveChain: Promise<unknown> = Promise.resolve();
let latestSaveId = 0;

function enqueueSave(
  save: (next: ITaskDefaults) => Promise<unknown>,
  pendingSave: PendingTaskDefaultsSave,
) {
  const run = saveChain
    .catch(() => undefined)
    .then(async () => {
      if (pendingSave.id !== latestSaveId) return;

      await save(pendingSave.defaults);
      if (pendingTaskDefaults === pendingSave.defaults) {
        pendingTaskDefaults = undefined;
      }
    });
  saveChain = run;
  return run;
}

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
  taskDefaultsRef.current = pendingTaskDefaults ?? taskDefaults;

  const flush = useDebounce(async (pendingSave: PendingTaskDefaultsSave) => {
    await enqueueSave(
      (value) =>
        configureUISetting({
          variables: { key: "taskDefaults", value },
        }),
      pendingSave,
    );
  }, 500);

  useEffect(() => {
    return () => {
      flush.flush();
    };
  }, [flush]);

  const save = useMemo(
    () =>
      function save<K extends TaskKey>(key: K, value: ITaskDefaults[K]) {
        const next = {
          ...(pendingTaskDefaults ?? taskDefaultsRef.current),
          [key]: value,
        };
        const id = latestSaveId + 1;
        latestSaveId = id;
        pendingTaskDefaults = next;
        taskDefaultsRef.current = next;
        flush({ id, defaults: next });
      },
    [flush],
  );

  return { taskDefaults, save };
}
