import React, { useCallback } from "react";
import { useMutation } from "@apollo/client/react";
import type { ApolloCache } from "@apollo/client/cache";
import * as GQL from "src/core/generated-graphql";
import { useSaveIndicator } from "./save-indicator";

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

export const useConfigureUISetting = () => {
  const saveIndicator = useSaveIndicator();
  const [mutate, result] = useMutation(GQL.ConfigureUiSettingDocument, {
    update: (cache, mutationResult) =>
      updateUIConfig(cache, mutationResult.data?.configureUISetting),
  });
  // Route every call through the save-indicator so the floating
  // spinner / check / X surfaces automatically. Apollo's `mutate`
  // returns a promise; wrapping it in `track()` lets the indicator
  // observe the outcome without changing the function signature.
  const trackedMutate = useCallback<typeof mutate>(
    (options) => saveIndicator.track(mutate(options)),
    [mutate, saveIndicator],
  );
  return [trackedMutate, result] as const;
};

// The configure* mutations return their sub-config result objects without
// an `id`, so Apollo's normalised cache can't merge them automatically.
// Each hook below patches the corresponding key of the cached
// `Configuration` query so `useConfigurationContext` consumers see the
// change without a refetch, and routes the promise through the
// save-indicator so the floating spinner / check / X surfaces.
function writeConfigKey(
  cache: ApolloCache,
  key: keyof GQL.ConfigDataFragment,
  updated: unknown,
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
  const saveIndicator = useSaveIndicator();
  const [mutate, result] = useMutation(GQL.ConfigureInterfaceDocument, {
    update: (cache, mutationResult) =>
      writeConfigKey(
        cache,
        "interface",
        mutationResult.data?.configureInterface,
      ),
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => saveIndicator.track(mutate(options)),
    [mutate, saveIndicator],
  );
  return [trackedMutate, result] as const;
};

export const useConfigureGeneral = () => {
  const saveIndicator = useSaveIndicator();
  const [mutate, result] = useMutation(GQL.ConfigureGeneralDocument, {
    update: (cache, mutationResult) =>
      writeConfigKey(cache, "general", mutationResult.data?.configureGeneral),
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => saveIndicator.track(mutate(options)),
    [mutate, saveIndicator],
  );
  return [trackedMutate, result] as const;
};

export const useConfigureDefaults = () => {
  const saveIndicator = useSaveIndicator();
  const [mutate, result] = useMutation(GQL.ConfigureDefaultsDocument, {
    update: (cache, mutationResult) =>
      writeConfigKey(cache, "defaults", mutationResult.data?.configureDefaults),
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => saveIndicator.track(mutate(options)),
    [mutate, saveIndicator],
  );
  return [trackedMutate, result] as const;
};

export const useConfigureScraping = () => {
  const saveIndicator = useSaveIndicator();
  const [mutate, result] = useMutation(GQL.ConfigureScrapingDocument, {
    update: (cache, mutationResult) =>
      writeConfigKey(cache, "scraping", mutationResult.data?.configureScraping),
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => saveIndicator.track(mutate(options)),
    [mutate, saveIndicator],
  );
  return [trackedMutate, result] as const;
};

export const useConfigureDLNA = () => {
  const saveIndicator = useSaveIndicator();
  const [mutate, result] = useMutation(GQL.ConfigureDlnaDocument, {
    update: (cache, mutationResult) =>
      writeConfigKey(cache, "dlna", mutationResult.data?.configureDLNA),
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => saveIndicator.track(mutate(options)),
    [mutate, saveIndicator],
  );
  return [trackedMutate, result] as const;
};

// Per-plugin settings (`configuration.plugins` map). The mutation returns
// the full updated plugins map.
export const useConfigurePlugin = () => {
  const saveIndicator = useSaveIndicator();
  const [mutate, result] = useMutation(GQL.ConfigurePluginDocument, {
    update: (cache, mutationResult) =>
      writeConfigKey(cache, "plugins", mutationResult.data?.configurePlugin),
  });
  const trackedMutate = useCallback<typeof mutate>(
    (options) => saveIndicator.track(mutate(options)),
    [mutate, saveIndicator],
  );
  return [trackedMutate, result] as const;
};
