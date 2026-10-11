import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import { mediaRoot, rootID } from "../fixtures/collections";
import {
  mediaRootInputSchema,
  type MediaRoot,
  type MediaRootInput,
  type MediaRootRevision,
} from "../../src/core/native-archive/media-root-api";

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.name = "library-folder-fixture";
  });
});

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
    if (url.pathname.endsWith("/page-info")) {
      const rows =
        url.searchParams.get("resource") === "root-history"
          ? (history.get(url.searchParams.get("subject") ?? "") ?? [])
          : [...roots.values()].filter(
              (root) =>
                (!url.searchParams.get("q") ||
                  root.label.includes(url.searchParams.get("q")!) ||
                  root.binding?.path.includes(url.searchParams.get("q")!)) &&
                (!url.searchParams.get("state") ||
                  root.state === url.searchParams.get("state")),
            );
      const cursor = url.searchParams.get("cursor");
      return route.fulfill({
        json: {
          total: rows.length,
          offset: cursor
            ? rows.filter((row) =>
                url.searchParams.get("resource") === "root-history"
                  ? row.revision <= Number(cursor)
                  : row.uuid <= cursor,
              ).length
            : 0,
        },
      });
    }
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
          scan: input.scan ?? mediaRoot().scan,
          unbound_path: input.unbound_path ?? "",
          revision: (() => {
            const before = roots.get(id);
            return before &&
              before.label === input.label &&
              before.state === input.state &&
              JSON.stringify(before.binding) ===
                JSON.stringify(input.binding) &&
              before.unbound_path === (input.unbound_path ?? "")
              ? before.revision
              : input.expected_revision + 1;
          })(),
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

test("configures a folder in Library settings and redirects the former page", async ({
  page,
}) => {
  const remote = await archive(page);
  await page.goto("/media-roots");
  await expect(page).toHaveURL(/\/settings\/library/);
  await page
    .getByRole("button", { name: "Add library folder", exact: true })
    .click();
  await page.getByLabel("Folder name", { exact: true }).fill("Purchased media");
  const path = page.getByLabel("Folder path on the Stash server", {
    exact: true,
  });
  await path.fill("relative/path");
  await page.getByRole("button", { name: "Save folder", exact: true }).click();
  await expect(
    page.getByText("Use an absolute server path", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toEqual([]);
  await path.fill("/media/purchased");
  await page
    .getByRole("switch", { name: "Scan images", exact: true })
    .uncheck();
  await page.getByRole("button", { name: "Save folder", exact: true }).click();
  await expect(page.getByText("Folder saved", { exact: true })).toBeVisible();
  expect(remote.writes).toHaveLength(1);
  expect(remote.writes[0]).toMatchObject({
    expected_revision: 0,
    label: "Purchased media",
    binding: {
      path: "/media/purchased",
      directory_identity: "directory:checked",
    },
    scan: { enabled: true, exclude_image: true, exclude_video: false },
  });
  expect(remote.writes[0]?.uuid).toMatch(/^[a-f\d-]{36}$/);
  await page.screenshot({
    path: test.info().outputPath("library-folder-editor.png"),
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "Open navigation menu", exact: true })
    .click();
  await expect(
    page
      .getByRole("dialog")
      .getByRole("link", { name: "Media roots", exact: true }),
  ).toHaveCount(0);
});

test("recovers a scan-settings save after a lost response without another write", async ({
  page,
}) => {
  const remote = await archive(page, { loseResponse: true });
  await page.goto(`/settings/library?root=${rootID}`);
  await page
    .getByRole("switch", { name: "Scan videos", exact: true })
    .uncheck();
  await page.getByRole("button", { name: "Save folder", exact: true }).click();
  await expect(
    page.getByText("Confirm this saved folder change", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Check and retry folder change", exact: true })
    .click();
  await expect(page.getByText("Folder saved", { exact: true })).toBeVisible();
  expect(remote.writes).toHaveLength(1);
  expect(remote.probes).toEqual([]);
  expect(remote.roots.get(rootID)?.revision).toBe(1);
  expect(remote.roots.get(rootID)?.scan.exclude_video).toBe(true);
  await expect(
    page.getByRole("switch", { name: "Scan videos", exact: true }),
  ).not.toBeChecked();
});

test("retains an offline folder path and loads storage history only when expanded", async ({
  page,
}) => {
  const remote = await archive(page);
  await page.goto(`/settings/library?root=${rootID}`);
  await page
    .getByRole("button", { name: "Advanced folder options", exact: true })
    .click();
  await page
    .getByRole("switch", { name: "Folder is offline", exact: true })
    .check();
  await page.getByRole("button", { name: "Save folder", exact: true }).click();
  await expect(page.getByText("Folder saved", { exact: true })).toBeVisible();
  expect(remote.writes[0]).toMatchObject({
    uuid: rootID,
    binding: null,
    unbound_path: "/media/library",
    scan: { enabled: true },
  });
  expect(remote.probes).toEqual([]);
  expect(
    remote.requests.filter((url) => url.pathname.endsWith("/history")),
  ).toHaveLength(0);
  await page
    .getByRole("button", { name: "Storage details and history", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Folder binding history", exact: true })
    .click();
  await expect(page.getByText("Initial root", { exact: true })).toBeVisible();
});

test("rejects a replaced directory without silently changing the folder", async ({
  page,
}) => {
  const remote = await archive(page, { invalidBinding: true });
  await page.goto(`/settings/library?root=${rootID}`);
  await page
    .getByLabel("Folder path on the Stash server", { exact: true })
    .fill("/new/location");
  await page.getByRole("button", { name: "Save folder", exact: true }).click();
  await expect(
    page.getByText("The directory changed after it was checked", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Save folder", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Review current folder", exact: true })
    .click();
  await expect(
    page.getByLabel("Folder path on the Stash server", { exact: true }),
  ).toHaveValue("/media/library");
  expect(remote.writes).toHaveLength(1);
});
