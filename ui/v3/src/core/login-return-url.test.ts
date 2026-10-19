import { expect, it } from "vitest";
import { loginReturnURL } from "./login-return-url";

it("preserves filters and fragments within the current deployment", () => {
  const base = new URL("https://library.test/stash/");
  expect(loginReturnURL("/stash/scenes?filter=a%26b#selected", base)).toBe(
    "https://library.test/stash/scenes?filter=a%26b#selected",
  );
  expect(
    loginReturnURL("https://library.test/stash/settings/security", base),
  ).toBe("https://library.test/stash/settings/security");
});

it("rejects foreign origins, credentials, other mounts and login loops", () => {
  const base = new URL("https://library.test/stash/");
  for (const value of [
    null,
    "",
    "//outside.test/",
    "https://outside.test/stash/",
    "https://user@library.test/stash/",
    "/other/",
    "/stash-other/",
    "/stash/../outside",
    "/stash/login",
    "javascript:alert(1)",
  ]) {
    expect(loginReturnURL(value, base), String(value)).toBe(base.href);
  }
});
