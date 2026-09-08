import React, { useCallback } from "react";
import { useMutation } from "@apollo/client/react";
import type { ApolloCache } from "@apollo/client/cache";
import * as GQL from "src/core/generated-graphql";
import { useSaveIndicator } from "./save-indicator";
import { useToast } from "./toast";

export interface IContext {
  configuration: GQL.ConfigDataFragment;
}

export const ConfigurationContext = React.createContext<IContext | null>(null);

export const useConfigurationContext = () => {
  const context = React.useContext(ConfigurationContext);

  if (context === null) {
    throw new Error(
      "useConfigurationContext must be used within a ConfigurationProvider",
    );
  }

  return context;
};

export const useConfigurationContextOptional = () => {
  return React.useContext(ConfigurationContext);
};

export const ConfigurationProvider: React.FC<
  React.PropsWithChildren<IContext>
> = ({ configuration, children }) => {
  return (
    <ConfigurationContext.Provider
      value={{
        configuration,
      }}
    >
      {children}
    </ConfigurationContext.Provider>
  );
};

function updateUIConfig(
  cache: ApolloCache,
  result: GQL.ConfigureUiMutation["configureUI"] | undefined,
) {
  if (!result) return;

  const existing = cache.readQuery<GQL.ConfigurationQuery>({
    query: GQL.ConfigurationDocument,
  });

  if (!existing?.configuration) return;

  cache.writeQuery({
    query: GQL.ConfigurationDocument,
    data: {
      configuration: {
        ...existing.configuration,
        ui: result,
      },
    },
  });
}

/**
 * Wraps a config-save promise so the floating save-indicator
 * (spinner / check / X) observes it, and toasts the error message on
 * failure — the indicator's X only says *that* a save failed, not why.
 * The original promise is returned, so callers that await it still
 * observe rejection themselves; fire-and-forget (`void`) callers are
 * covered by the side-chain handler.
 */
function useTrackedSave() {
  const saveIndicator = useSaveIndicator();
  const Toast = useToast();
  return useCallback(
    <T,>(promise: Promise<T>): Promise<T> => {
      const tracked = saveIndicator.track(promise);
      tracked.catch((e: unknown) => Toast.error(e));
      return tracked;
    },
    [saveIndicator, Toast],
  );
}

export const useConfigureUISetting = () => {
  const trackSave = useTrackedSave();
  const [mutate, result] = useMutation(GQL.ConfigureUiSettingDocument, {
    update: (cache, mutationResult) =>
      updateUIConfig(cache, mutationResult.data?.configureUISetting),
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => trackSave(mutate(options)),
    [mutate, trackSave],
  );
  return [trackedMutate, result] as const;
};

export const useConfigureUI = () => {
  const trackSave = useTrackedSave();
  const [mutate, result] = useMutation(GQL.ConfigureUiDocument, {
    update: (cache, mutationResult) =>
      updateUIConfig(cache, mutationResult.data?.configureUI),
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => trackSave(mutate(options)),
    [mutate, trackSave],
  );
  return [trackedMutate, result] as const;
};

export const useConfigureDefaultFilter = () => {
  const trackSave = useTrackedSave();
  const [mutate, result] = useMutation(GQL.ConfigureDefaultFilterDocument, {
    update: (cache, mutationResult) =>
      updateUIConfig(cache, mutationResult.data?.configureDefaultFilter),
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => trackSave(mutate(options)),
    [mutate, trackSave],
  );
  return [trackedMutate, result] as const;
};

// The configure* mutations return their sub-config result objects without
// an `id`, so Apollo's normalised cache can't merge them automatically.
// Each hook below patches the corresponding key of the cached
// `Configuration` query so `useConfigurationContext` consumers see the
// change without a refetch, and routes the promise through the
// save-indicator so the floating spinner / check / X surfaces.
export function writeConfigKey<Key extends keyof GQL.ConfigDataFragment>(
  cache: ApolloCache,
  key: Key,
  updated: GQL.ConfigDataFragment[Key] | null | undefined,
) {
  if (!updated) return;
  const existing = cache.readQuery<GQL.ConfigurationQuery>({
    query: GQL.ConfigurationDocument,
  });
  if (!existing?.configuration) return;
  cache.writeQuery({
    query: GQL.ConfigurationDocument,
    data: {
      configuration: {
        ...existing.configuration,
        [key]: updated,
      },
    },
  });
}

export const useConfigureInterface = () => {
  const trackSave = useTrackedSave();
  const [mutate, result] = useMutation(GQL.ConfigureInterfaceDocument, {
    update: (cache, mutationResult) =>
      writeConfigKey(
        cache,
        "interface",
        mutationResult.data?.configureInterface,
      ),
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => trackSave(mutate(options)),
    [mutate, trackSave],
  );
  return [trackedMutate, result] as const;
};

export const useConfigureGeneral = () => {
  const trackSave = useTrackedSave();
  const [mutate, result] = useMutation(GQL.ConfigureGeneralDocument, {
    update: (cache, mutationResult) =>
      writeConfigKey(cache, "general", mutationResult.data?.configureGeneral),
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => trackSave(mutate(options)),
    [mutate, trackSave],
  );
  return [trackedMutate, result] as const;
};

export const useConfigureDefaults = () => {
  const trackSave = useTrackedSave();
  const [mutate, result] = useMutation(GQL.ConfigureDefaultsDocument, {
    update: (cache, mutationResult) =>
      writeConfigKey(cache, "defaults", mutationResult.data?.configureDefaults),
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => trackSave(mutate(options)),
    [mutate, trackSave],
  );
  return [trackedMutate, result] as const;
};

export const useConfigureScraping = () => {
  const trackSave = useTrackedSave();
  const [mutate, result] = useMutation(GQL.ConfigureScrapingDocument, {
    update: (cache, mutationResult) =>
      writeConfigKey(cache, "scraping", mutationResult.data?.configureScraping),
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => trackSave(mutate(options)),
    [mutate, trackSave],
  );
  return [trackedMutate, result] as const;
};

export const useConfigureDLNA = () => {
  const trackSave = useTrackedSave();
  const [mutate, result] = useMutation(GQL.ConfigureDlnaDocument, {
    update: (cache, mutationResult) =>
      writeConfigKey(cache, "dlna", mutationResult.data?.configureDLNA),
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => trackSave(mutate(options)),
    [mutate, trackSave],
  );
  return [trackedMutate, result] as const;
};

// configurePlugin returns one plugin's settings, not the plugins map.
export const useConfigurePlugin = () => {
  const trackSave = useTrackedSave();
  const [mutate, result] = useMutation(GQL.ConfigurePluginDocument, {
    update: (cache, mutationResult, { variables }) => {
      const updated = mutationResult.data?.configurePlugin;
      if (!updated || !variables) return;
      const existing = cache.readQuery({ query: GQL.ConfigurationDocument });
      if (!existing?.configuration) return;
      writeConfigKey(cache, "plugins", {
        ...existing.configuration.plugins,
        [variables.plugin_id]: updated,
      });
    },
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => trackSave(mutate(options)),
    [mutate, trackSave],
  );
  return [trackedMutate, result] as const;
};
