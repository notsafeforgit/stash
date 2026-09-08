import { z } from "zod";

export const lightboxSettingsSchema = z.looseObject({
  scrollToZoom: z.boolean().catch(false),
  displayMode: z.enum(["fitXY", "fitX", "original"]).catch("fitXY"),
  slideshowDelay: z.number().positive().finite().catch(5),
});
export type LightboxSettings = z.infer<typeof lightboxSettingsSchema>;
const key = "stash_lightbox_settings";

export function loadSettings(): LightboxSettings {
  try {
    return lightboxSettingsSchema.parse(
      JSON.parse(localStorage.getItem(key) ?? "{}"),
    );
  } catch {
    return lightboxSettingsSchema.parse({});
  }
}

export function persistSettings(settings: LightboxSettings) {
  try {
    localStorage.setItem(key, JSON.stringify(settings));
  } catch {
    // Settings still work for the current viewer when persistence is disabled.
  }
}
