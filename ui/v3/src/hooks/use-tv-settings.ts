import { useCallback, useMemo, useState, useSyncExternalStore } from "react";
import { useApolloClient } from "@apollo/client/react";
import type { ApolloClient } from "@apollo/client";
import { useConfigurationContext, useConfigureUISetting } from "./config";
import { createStoredState } from "./stored-state";
import { getPlatformURL } from "@/core/platform-url";
import {
  decodeTvSettings,
  defaultTvSettings,
  tvRotationSchema,
  tvSettingsSchema,
  type TvSettings,
} from "@/core/tv/settings";

type SaveSnapshot = {
  draft: TvSettings | undefined;
  failed: boolean;
};
const savedSnapshot: SaveSnapshot = { draft: undefined, failed: false };
type PendingSettings = {
  snapshot: SaveSnapshot;
  chain: Promise<void>;
  revision: number;
  listeners: Set<() => void>;
};
const pending = new WeakMap<ApolloClient, PendingSettings>();
export function createTvRotationStore() {
  return createStoredState(
    `stash:tv:rotation:v1:${getPlatformURL().href}`,
    tvRotationSchema,
    "normal",
  );
}
function stateFor(client: ApolloClient): PendingSettings {
  let state = pending.get(client);
  if (!state) {
    state = {
      snapshot: savedSnapshot,
      chain: Promise.resolve(),
      revision: 0,
      listeners: new Set(),
    };
    pending.set(client, state);
  }
  return state;
}

function publish(state: PendingSettings, snapshot: SaveSnapshot) {
  state.snapshot = snapshot;
  for (const notify of state.listeners) notify();
}

/** One serialized TV-key writer per backend client. Drafts survive a route
 * handoff while saves settle; unrelated UI configuration is never replaced. */
export function useTvSettings() {
  const client = useApolloClient();
  const { configuration } = useConfigurationContext();
  const [configure] = useConfigureUISetting();
  const subscribe = useCallback(
    (listener: () => void) => {
      const state = stateFor(client);
      state.listeners.add(listener);
      return () => {
        state.listeners.delete(listener);
      };
    },
    [client],
  );
  const getSnapshot = useCallback(
    () => pending.get(client)?.snapshot ?? savedSnapshot,
    [client],
  );
  const { draft, failed } = useSyncExternalStore(subscribe, getSnapshot);
  const result = useMemo(
    () => decodeTvSettings(draft ?? configuration.ui.tv),
    [draft, configuration.ui.tv],
  );
  const save = useCallback(
    (next: TvSettings) => {
      const parsed = tvSettingsSchema.parse(next);
      const state = stateFor(client);
      const revision = ++state.revision;
      publish(state, { draft: parsed, failed: false });
      const operation = state.chain
        .then(async () => {
          if (state.revision !== revision) return false;
          await configure({ variables: { key: "tv", value: parsed } });
          if (state.revision === revision) {
            publish(state, savedSnapshot);
          }
          return true;
        })
        .catch(() => {
          /* Tracked save reports failure; retain the recoverable draft. */
          if (state.revision === revision)
            publish(state, { draft: parsed, failed: true });
          return false;
        });
      state.chain = operation.then(() => {});
      return operation;
    },
    [client, configure],
  );
  const [rotationStore] = useState(createTvRotationStore);
  const [rotation, setRotation] = rotationStore.useStoredState();
  return {
    result,
    save,
    failed,
    retry: () => (draft ? save(draft) : Promise.resolve(false)),
    reset: () => save(defaultTvSettings),
    rotation,
    setRotation,
  };
}
