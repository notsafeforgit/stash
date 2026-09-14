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

type PendingSettings = {
  draft: TvSettings | undefined;
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
      draft: undefined,
      chain: Promise.resolve(),
      revision: 0,
      listeners: new Set(),
    };
    pending.set(client, state);
  }
  return state;
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
  const getSnapshot = useCallback(() => pending.get(client)?.draft, [client]);
  const draft = useSyncExternalStore(subscribe, getSnapshot);
  const result = useMemo(
    () => decodeTvSettings(draft ?? configuration.ui.tv),
    [draft, configuration.ui.tv],
  );
  const save = useCallback(
    (next: TvSettings) => {
      const parsed = tvSettingsSchema.parse(next);
      const state = stateFor(client);
      state.draft = parsed;
      const revision = ++state.revision;
      for (const notify of state.listeners) notify();
      const operation = state.chain
        .then(async () => {
          if (state.revision !== revision) return false;
          await configure({ variables: { key: "tv", value: parsed } });
          if (state.revision === revision) {
            state.draft = undefined;
            for (const notify of state.listeners) notify();
          }
          return true;
        })
        .catch(() => {
          /* Tracked save reports failure; retain the recoverable draft. */
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
    reset: () => save(defaultTvSettings),
    rotation,
    setRotation,
  };
}
