import { expect, it } from "vitest";
import { taskDefaultsSchema } from "./task-defaults";

it("never restores a cover reset from remembered generation settings", () => {
  const defaults = taskDefaultsSchema.parse({
    generate: {
      covers: true,
      overwrite: true,
      resetCoversToDefault: true,
      sceneSelection: { find_filter: { q: "previous action" } },
    },
  });
  expect(defaults.generate?.covers).toBe(true);
  expect(defaults.generate?.overwrite).toBe(true);
  expect(defaults.generate?.resetCoversToDefault).toBeUndefined();
  expect(defaults.generate?.sceneSelection).toBeUndefined();
});
