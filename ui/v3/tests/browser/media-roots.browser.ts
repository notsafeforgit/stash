import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import { mediaRoot, rootID } from "../fixtures/collections";
import {
  mediaRootInputSchema,
  type MediaRoot,
  type MediaRootInput,
  type MediaRootRevision,
} from "../../src/core/native-archive/media-root-api";

test.use({
  expectedConsoleErrors: [
    "the server responded with a status of 400",
    "the server responded with a status of 404",
    "the server responded with a status of 409",
    "the server responded with a status of 503",
  ],
});

async function archive(
  page: Page,
  options: {
    loseResponse?: boolean;
    invalidBinding?: boolean;
    refreshFails?: boolean;
    retired?: boolean;
  } = {},
) {
  const roots = new Map<string, MediaRoot>([
    [rootID, { ...mediaRoot(), state: options.retired ? "retired" : "active" }],
  ]);
  const history = new Map<string, MediaRootRevision[]>([
    [
      rootID,
      [
        {
          ...mediaRoot(),
          state: options.retired ? "retired" : "active",
          origin: "review",
          reason: "Initial root",
          recorded_at: "2026-10-05T21:00:00Z",
        },
      ],
    ],
  ]);
  const writes: MediaRootInput[] = [];
  const probes: string[] = [];
  const requests: URL[] = [];
  let loseResponse = options.loseResponse;
  let invalidBinding = options.invalidBinding;
  let refreshFails = false;
  await page.route("**/api/v3/archive/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace(/^.*\/api\/v3\/archive\//, "");
    requests.push(url);
    if (path === "media-roots/probe") {
      const input: { server_path: string } = route.request().postDataJSON();
      probes.push(input.server_path);
      if (!input.server_path.startsWith("/"))
        return route.fulfill({
          status: 400,
          json: {
            error: "invalid_root_binding",
            message: "Use an absolute server path",
          },
        });
      return route.fulfill({
        json: {
          path: input.server_path,
          directory_identity: "directory:checked",
        },
      });
    }
    if (path === "media-roots") {
      const q = url.searchParams.get("q");
      const state = url.searchParams.get("state");
      return route.fulfill({
        json: [...roots.values()]
          .sort((a, b) => (a.uuid < b.uuid ? -1 : a.uuid > b.uuid ? 1 : 0))
          .filter(
            (root) =>
              (!q ||
                root.label.includes(q) ||
                root.binding?.path.includes(q)) &&
              (!state || state === root.state),
          ),
      });
    }
    const [, id, tail] = path.split("/");
    if (path.startsWith("media-roots/") && id) {
      if (route.request().method() === "PUT") {
        const input = mediaRootInputSchema.parse(
          route.request().postDataJSON(),
        );
        expect(input.uuid).toBe(id);
        writes.push(input);
        if (invalidBinding) {
          invalidBinding = false;
          return route.fulfill({
            status: 400,
            json: {
              error: "invalid_root_binding",
              message: "The directory changed after it was checked",
            },
          });
        }
        const found: MediaRoot = {
          ...mediaRoot(),
          uuid: id,
          label: input.label,
          state: input.state,
          binding: input.binding,
          revision: input.expected_revision + 1,
        };
        roots.set(id, found);
        history.set(id, [
          ...(history.get(id) ?? []),
          {
            ...found,
            origin: "review",
            reason: input.reason,
            recorded_at: "2026-10-05T22:00:00Z",
          },
        ]);
        refreshFails = !!options.refreshFails;
        if (loseResponse) {
          loseResponse = false;
          return route.fulfill({ status: 503, json: { error: "unavailable" } });
        }
        return route.fulfill({ json: found });
      }
      if (!roots.has(id))
        return route.fulfill({ status: 404, json: { error: "not_found" } });
      if (tail === "history")
        return route.fulfill({
          json: (history.get(id) ?? [])
            .filter(
              (row) => row.revision > Number(url.searchParams.get("after")),
            )
            .slice(0, Number(url.searchParams.get("limit"))),
        });
      if (refreshFails) {
        refreshFails = false;
        return route.fulfill({ status: 503, json: { error: "unavailable" } });
      }
      return route.fulfill({ json: roots.get(id) });
    }
    throw new Error(`Unexpected root request ${path}`);
  });
  return { roots, writes, probes, requests };
}

test("registers a checked folder with a stable caller UUID and exposes mobile navigation", async ({
  page,
}) => {
  const remote = await archive(page);
  await page.goto("/media-roots");
  await page
    .getByRole("button", { name: "New media root", exact: true })
    .click();
  await page.getByLabel("Root name", { exact: true }).fill("Purchased media");
  await page
    .getByRole("switch", {
      name: "Associate a folder on this server",
      exact: true,
    })
    .check();
  const path = page.getByLabel("Folder path on the Stash server", {
    exact: true,
  });
  await path.fill("relative/path");
  await page.getByRole("button", { name: "Check folder", exact: true }).click();
  await expect(
    page.getByText("Use an absolute server path", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Save media root", exact: true }),
  ).toBeDisabled();
  expect(remote.writes).toEqual([]);
  await path.fill("/media/purchased");
  await page.getByRole("button", { name: "Check folder", exact: true }).click();
  await expect(page.getByText("Folder checked", { exact: true })).toBeVisible();
  expect(remote.writes).toEqual([]);
  await path.fill("/media/changed");
  await expect(
    page.getByRole("button", { name: "Save media root", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Check folder", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Save media root", exact: true }),
  ).toBeEnabled();
  await page
    .getByRole("button", { name: "Save media root", exact: true })
    .click();
  await expect(
    page.getByText("Media root saved", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(1);
  expect(remote.writes[0]).toMatchObject({
    expected_revision: 0,
    label: "Purchased media",
    binding: {
      path: "/media/changed",
      directory_identity: "directory:checked",
    },
  });
  expect(remote.writes[0]?.uuid).toMatch(/^[a-f\d-]{36}$/);
  await page
    .getByRole("button", { name: "Open navigation menu", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("link", { name: "Media roots", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByText("Purchased media", { exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: test.info().outputPath("media-roots-mobile.png"),
    fullPage: true,
  });
});

test("recovers a committed root update without a new probe or PUT and loads historical bindings lazily", async ({
  page,
}) => {
  const remote = await archive(page, { loseResponse: true });
  await page.goto(`/media-roots?root=${rootID}`);
  await page.getByLabel("Root name", { exact: true }).fill("Renamed root");
  expect(remote.requests.some((url) => url.pathname.endsWith("/history"))).toBe(
    false,
  );
  await page
    .getByRole("button", { name: "Save media root", exact: true })
    .click();
  await expect(
    page.getByText("Could not complete this step", { exact: true }),
  ).toBeVisible();
  await expect.poll(() => remote.writes.length).toBe(1);
  await page.reload();
  await page
    .getByRole("button", { name: "Check and retry root change", exact: true })
    .click();
  await expect(
    page.getByText("Media root saved", { exact: true }),
  ).toBeVisible();
  expect(remote.probes).toEqual([]);
  expect(remote.writes).toHaveLength(1);
  await page
    .getByRole("button", { name: "Media root history", exact: true })
    .click();
  await expect(page.getByText("Revision 2", { exact: false })).toBeVisible();
  expect(
    remote.requests
      .filter((url) => url.pathname.endsWith("/history"))
      .at(-1)
      ?.searchParams.get("limit"),
  ).toBe("25");
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.screenshot({
    path: test.info().outputPath("media-root-editor-desktop.png"),
    fullPage: true,
  });
});

test("rejects a changed directory and requires review before another save", async ({
  page,
}) => {
  const remote = await archive(page, { invalidBinding: true });
  await page.goto(`/media-roots?root=${rootID}`);
  await page
    .getByLabel("Folder path on the Stash server", { exact: true })
    .fill("/new/location");
  await page.getByRole("button", { name: "Check folder", exact: true }).click();
  await expect(page.getByText("Folder checked", { exact: true })).toBeVisible();
  await page
    .getByRole("button", { name: "Save media root", exact: true })
    .click();
  await expect(
    page.getByText("The directory changed after it was checked", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Save media root", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Review current root", exact: true })
    .click();
  await expect(
    page.getByLabel("Folder path on the Stash server", { exact: true }),
  ).toHaveValue("/media/library");
  expect(remote.writes).toHaveLength(1);
  expect(remote.probes).toEqual(["/new/location"]);
});

test("disables an offline root without probing and retains success when refresh fails", async ({
  page,
}) => {
  const remote = await archive(page, { refreshFails: true });
  await page.goto(`/media-roots?root=${rootID}`);
  await page
    .getByRole("group", { name: "Status", exact: true })
    .getByRole("button", { name: "Disabled", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Save media root", exact: true })
    .click();
  await expect(
    page.getByText("The root is saved, but this view could not be refreshed", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByText("Media root saved", { exact: true }),
  ).toBeVisible();
  expect(remote.probes).toEqual([]);
  expect(remote.writes[0]).toMatchObject({
    state: "disabled",
    binding: mediaRoot().binding,
  });
});

for (const [label, state] of [
  ["Active", "active"],
  ["Disabled", "disabled"],
] as const) {
  test(`restores a retired root as ${state} with the same identity, binding and history`, async ({
    page,
  }) => {
    const remote = await archive(page, { retired: true });
    await page.goto(`/media-roots?root=${rootID}`);
    await expect(page.getByLabel("Root name", { exact: true })).toBeEnabled();
    const status = page.getByRole("group", { name: "Status", exact: true });
    await expect(
      status.getByRole("button", { name: "Retired", exact: true }),
    ).toHaveAttribute("aria-pressed", "true");
    await page
      .getByRole("button", { name: "Media root history", exact: true })
      .click();
    await expect(page.getByText("Initial root", { exact: true })).toBeVisible();
    expect(remote.writes).toEqual([]);
    expect(remote.probes).toEqual([]);

    await status.getByRole("button", { name: label, exact: true }).click();
    const reason = `Restore as ${state}`;
    await page
      .getByLabel("Reason for this change (optional)", { exact: true })
      .fill(reason);
    expect(remote.writes).toEqual([]);
    await page
      .getByRole("button", { name: "Save media root", exact: true })
      .click();
    await expect(
      page.getByText("Media root saved", { exact: true }),
    ).toBeVisible();
    await expect(
      status.getByRole("button", { name: label, exact: true }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(remote.writes).toEqual([
      {
        uuid: rootID,
        expected_revision: mediaRoot().revision,
        label: mediaRoot().label,
        state,
        binding: mediaRoot().binding,
        reason,
      },
    ]);
    expect(remote.roots.get(rootID)).toMatchObject({
      uuid: rootID,
      revision: mediaRoot().revision + 1,
      state,
      binding: mediaRoot().binding,
    });
    expect(remote.probes).toEqual([]);
    await page
      .getByRole("button", { name: "Media root history", exact: true })
      .click();
    await expect(page.getByText("Initial root", { exact: true })).toBeVisible();
    await expect(page.getByText(reason, { exact: true })).toBeVisible();
  });
}
