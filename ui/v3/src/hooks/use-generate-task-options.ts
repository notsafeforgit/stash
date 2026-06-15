import type * as GQL from "src/core/generated-graphql";
import { withoutTypename } from "src/utils/data";
import { useConfigurationContext } from "./config";
import { useTaskOptions } from "./use-task-options";

const fallbackGenerateOptions: GQL.GenerateMetadataInput = {
  covers: true,
  sprites: true,
  phashes: true,
  previews: true,
  markers: true,
};

export function useGenerateTaskOptions() {
  const { configuration } = useConfigurationContext();

  return useTaskOptions("generate", () =>
    configuration.defaults.generate
      ? (withoutTypename(
          configuration.defaults.generate,
        ) as GQL.GenerateMetadataInput)
      : fallbackGenerateOptions,
  );
}
