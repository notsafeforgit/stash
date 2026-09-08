import { expect, it } from "vitest";
import { frontPageContentSchema, parseUIConfig } from "./config-schema";
import { getFrontPageContent } from "./config";
import { interfacePreferencesSchema } from "@/hooks/interface-preferences";
import { lightboxSettingsSchema } from "@/components/lightbox/settings";

it.each([
  "",
  " ",
  "0",
  "-1",
  "1.5",
  "1e2",
  "12junk",
  0,
  -1,
  1.5,
  true,
  null,
])("rejects malformed saved-filter IDs without coercing %j", (savedFilterId) => {
  expect(
    frontPageContentSchema.safeParse([
      { __typename: "SavedFilter", savedFilterId },
    ]).success,
  ).toBe(false);
});

it("preserves an intentionally empty Home Screen", () => {
  expect(getFrontPageContent(parseUIConfig({ frontPageContent: [] }))).toEqual(
    [],
  );
});

it("validates known configuration without stripping extension fields", () => {
  const value = parseUIConfig({
    previewVolume: "loud",
    advancedMode: true,
    tableColumns: 12,
    futurePlugin: { nested: [1] },
  });
  expect(value.previewVolume).toBeUndefined();
  expect(value.advancedMode).toBe(true);
  expect(value.tableColumns).toBeUndefined();
  expect(value).toHaveProperty("futurePlugin.nested", [1]);
  expect(parseUIConfig(null)).toEqual({});
  expect(
    getFrontPageContent({ frontPageContent: { invalid: true } }),
  ).toBeUndefined();
});

it("supplies real defaults for missing and invalid persisted settings", () => {
  expect(interfacePreferencesSchema.parse(null)).toEqual({
    queryConfig: {},
    viewConfig: {},
    imageLightbox: {},
  });
  expect(
    interfacePreferencesSchema.parse({
      viewConfig: { scenes: { showSidebar: "yes", extension: 1 } },
    }).viewConfig.scenes,
  ).toEqual({ showSidebar: undefined, extension: 1 });
  expect(
    lightboxSettingsSchema.parse({
      displayMode: "invalid",
      slideshowDelay: -1,
      scrollToZoom: "true",
    }),
  ).toEqual({ displayMode: "fitXY", slideshowDelay: 5, scrollToZoom: false });
});

it("repairs task defaults while retaining unknown settings from other clients", () => {
  const parsed = parseUIConfig({
    taskDefaults: {
      scan: { paths: "invalid", rescan: true, extension: 1 },
      clean: { dryRun: "invalid" },
      generate: {
        previews: "yes",
        previewOptions: { previewSegments: -1, future: true },
      },
      futureTask: { extra: "keep" },
    },
  });
  expect(parsed.taskDefaults?.scan).toMatchObject({
    rescan: true,
    extension: 1,
  });
  expect(parsed.taskDefaults?.scan?.paths).toBeUndefined();
  expect(parsed.taskDefaults?.clean?.dryRun).toBe(true);
  expect(parsed.taskDefaults?.generate?.previews).toBeUndefined();
  expect(parsed.taskDefaults).toHaveProperty(
    "generate.previewOptions.future",
    true,
  );
  expect(parsed.taskDefaults).toHaveProperty("futureTask.extra", "keep");
});
