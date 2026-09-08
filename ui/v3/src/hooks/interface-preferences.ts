import { z } from "zod";
import {
  ImageLightboxDisplayMode,
  ImageLightboxScrollMode,
} from "@/core/generated-graphql";
import { createStoredState } from "./stored-state";

const viewConfigSchema = z.looseObject({
  showSidebar: z.boolean().optional().catch(undefined),
});
export type ViewConfig = z.infer<typeof viewConfigSchema>;

// Keep the legacy keys/shape, including fields owned by other clients. Missing
// required v3 collections get real defaults; invalid known fields are repaired.
export const interfacePreferencesSchema = z
  .looseObject({
    queryConfig: z
      .record(
        z.string(),
        z.looseObject({
          filter: z.string().catch(""),
          itemsPerPage: z.number().int().positive().catch(40),
          currentPage: z.number().int().positive().catch(1),
        }),
      )
      .catch({}),
    imageLightbox: z
      .looseObject({
        disableAnimation: z.boolean().nullish().catch(undefined),
        displayMode: z
          .enum(ImageLightboxDisplayMode)
          .nullish()
          .catch(undefined),
        resetZoomOnNav: z.boolean().nullish().catch(undefined),
        scaleUp: z.boolean().nullish().catch(undefined),
        scrollAttemptsBeforeChange: z
          .number()
          .int()
          .nonnegative()
          .nullish()
          .catch(undefined),
        scrollMode: z.enum(ImageLightboxScrollMode).nullish().catch(undefined),
        slideshowDelay: z.number().int().positive().nullish().catch(undefined),
      })
      .catch({}),
    viewConfig: z.record(z.string(), viewConfigSchema.catch({})).catch({}),
  })
  .catch({ queryConfig: {}, imageLightbox: {}, viewConfig: {} });

export const interfacePreferences = createStoredState(
  "interface",
  interfacePreferencesSchema,
  interfacePreferencesSchema.parse({}),
);
export const readInterfaceConfig = interfacePreferences.getSnapshot;
export const useInterfacePreferences = interfacePreferences.useStoredState;
