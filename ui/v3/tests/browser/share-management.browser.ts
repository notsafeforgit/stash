import { libraryID } from "../fixtures/library";
import * as GQL from "../../src/core/generated-graphql";
import { test, expect } from "./test";
import { z } from "zod";
import type { Locator } from "@playwright/test";

test.use({ timezoneId: "America/Los_Angeles" });
test.beforeEach(async ({ page }) => {
  await page.route("**/api/v3/archive/page-info?**", (route) =>
    route.fulfill({ json: { total: 0, offset: 0 } }),
  );
});

test("share target search uses library titles, filenames and gallery folders", async ({
  page,
}) => {
  await page.route("**/graphql", async (route) => {
    const { operationName } = z
      .object({ operationName: z.string() })
      .parse(route.request().postDataJSON());
    if (operationName === "MediaShares") {
      const data: GQL.MediaSharesQuery = {
        mediaShares: [],
        sharingConfiguration: {
          __typename: "SharingConfiguration",
          public_url: "https://shares.test/share",
          max_days: 30,
          max_items: 2000,
        },
      };
      await route.fulfill({ json: { data } });
    } else if (operationName === "ShareTargetSearch") {
      const data: GQL.ShareTargetSearchQuery = {
        findScenes: {
          __typename: "FindScenesResultType",
          scenes: [
            {
              __typename: "Scene",
              id: libraryID(1),
              title: "Saved scene title",
              files: [
                {
                  __typename: "VideoFile",
                  path: "/private/library/Unused filename.mp4",
                },
              ],
            },
            {
              __typename: "Scene",
              id: libraryID(2),
              title: "",
              files: [
                {
                  __typename: "VideoFile",
                  path: "/private/library/Scene filename.mp4",
                },
              ],
            },
          ],
        },
        findImages: {
          __typename: "FindImagesResultType",
          images: [
            {
              __typename: "Image",
              id: libraryID(3),
              title: null,
              visual_files: [
                {
                  __typename: "ImageFile",
                  path: "C:\\private\\Photo filename.jpg",
                },
              ],
            },
          ],
        },
        findGalleries: {
          __typename: "FindGalleriesResultType",
          galleries: [
            {
              __typename: "Gallery",
              id: libraryID(4),
              title: "",
              image_count: 1,
              files: [],
              folder: {
                __typename: "Folder",
                path: "/private/library/Album folder",
                basename: "Album folder",
              },
            },
            {
              __typename: "Gallery",
              id: libraryID(5),
              title: null,
              image_count: 1,
              files: [
                {
                  __typename: "GalleryFile",
                  path: "/private/library/Archive filename.zip",
                },
              ],
              folder: null,
            },
          ],
        },
      };
      await route.fulfill({ json: { data } });
    } else throw new Error(`Unexpected operation ${operationName}`);
  });
  await page.goto("/share-management.html");
  await page.getByRole("button", { name: "Create share", exact: true }).click();
  const form = page.getByRole("dialog", { name: "Create share", exact: true });
  const names = [
    "Scene: Saved scene title",
    "Scene: Scene filename",
    "Image: Photo filename",
    "Gallery: Album folder",
    "Gallery: Archive filename",
  ];
  for (const name of names) {
    await form.getByPlaceholder("Search scenes, images and galleries…").click();
    await page.getByRole("option", { name, exact: true }).click();
  }
  await page.keyboard.press("Escape");
  await expect(form.locator('[data-slot="combobox-chip"]')).toHaveText(names);
});

async function expectDialogFits(dialog: Locator) {
  await expect(dialog).toBeInViewport({ ratio: 1 });
  await expect
    .poll(() =>
      dialog.evaluate((element) => element.scrollWidth - element.clientWidth),
    )
    .toBeLessThanOrEqual(1);
  for (const button of await dialog
    .locator('[data-slot="dialog-footer"] button')
    .all())
    await expect(button).toBeInViewport({ ratio: 1 });
  const form = dialog.locator("form");
  if (await form.count())
    await expect
      .poll(() =>
        form.evaluate((element) => element.scrollWidth - element.clientWidth),
      )
      .toBeLessThanOrEqual(1);
}

for (const older of [false, true]) {
  test(`saved links can be copied and previewed without rotation (${older ? "older" : "current"} share)`, async ({
    page,
    context,
  }, testInfo) => {
    await page.setViewportSize({ width: 320, height: 720 });
    const original =
      "https://shares.test/share/abcdefghijklmnopqrstuv/?key=original";
    const replacement =
      "https://shares.test/share/abcdefghijklmnopqrstuv/?key=replacement";
    let share: GQL.MediaShareFieldsFragment = {
      __typename: "MediaShare",
      id: "abcdefghijklmnopqrstuv",
      url: older ? null : original,
      label: "Saved recipient link",
      created_at: new Date().toISOString(),
      expires_at: new Date(Date.now() + 86_400_000).toISOString(),
      revoked_at: null,
      allow_download: false,
      show_metadata: false,
      access_count: 0,
      last_accessed_at: null,
      media_count: 1,
      items: [],
    };
    let rotations = 0;
    await page.addInitScript(() => {
      Object.defineProperty(navigator, "clipboard", {
        value: {
          writeText: async (value: string) => {
            sessionStorage.setItem("copied-share-url", value);
          },
        },
      });
    });
    await context.route("https://shares.test/share/**", (route) =>
      route.fulfill({
        contentType: "text/html",
        body: "<title>Shared media</title>",
      }),
    );
    await page.route("**/graphql", async (route) => {
      const request = z
        .object({ operationName: z.string(), variables: z.unknown() })
        .parse(route.request().postDataJSON());
      if (request.operationName === "MediaShares") {
        const data: GQL.MediaSharesQuery = {
          mediaShares: [share],
          sharingConfiguration: {
            __typename: "SharingConfiguration",
            public_url: "https://shares.test/share",
            max_days: 30,
            max_items: 2000,
          },
        };
        return route.fulfill({ json: { data } });
      }
      if (request.operationName === "MediaShareRotate") {
        expect(request.variables).toEqual({ id: share.id });
        rotations++;
        share = { ...share, url: replacement };
        return route.fulfill({
          json: { data: { mediaShareRotate: replacement } },
        });
      }
      throw new Error(`Unexpected operation ${request.operationName}`);
    });
    const menu = async () => {
      await page
        .getByRole("button", { name: "Operations", exact: true })
        .click();
    };
    const copied = () =>
      page.evaluate(() => sessionStorage.getItem("copied-share-url"));
    const preview = async (url: string, fromDialog = false) => {
      if (!fromDialog) await menu();
      const opened = context.waitForEvent("page");
      await page
        .getByRole(fromDialog ? "button" : "menuitem", {
          name: "Recipient preview",
          exact: true,
        })
        .click();
      const tab = await opened;
      await tab.waitForURL(url);
      expect(await tab.evaluate(() => window.opener === null)).toBe(true);
      await tab.close();
    };
    await page.goto("/share-management.html");
    await menu();
    await expect(page.getByRole("menu")).toBeVisible();
    await page.screenshot({
      path: testInfo.outputPath("saved-link-menu.png"),
      animations: "disabled",
    });
    if (older) {
      await expect(
        page.getByRole("menuitem", { name: "Copy link", exact: true }),
      ).toBeDisabled();
      await expect(
        page.getByRole("menuitem", { name: "Recipient preview", exact: true }),
      ).toBeDisabled();
      await expect(
        page.getByText(/Links you already sent still work/),
      ).toBeVisible();
      await page.keyboard.press("Escape");
    } else {
      await page
        .getByRole("menuitem", { name: "Copy link", exact: true })
        .click();
      await expect.poll(copied).toBe(original);
      await preview(original);
      await page.reload();
      await menu();
      await page
        .getByRole("menuitem", { name: "Copy link", exact: true })
        .click();
      await expect.poll(copied).toBe(original);
    }
    expect(rotations).toBe(0);
    await menu();
    await page
      .getByRole("menuitem", { name: "Regenerate link", exact: true })
      .click();
    await expect(
      page.getByText(/The old link and every active session will stop working/),
    ).toBeVisible();
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    expect(rotations).toBe(0);
    await menu();
    await page
      .getByRole("menuitem", { name: "Regenerate link", exact: true })
      .click();
    await page.getByRole("button", { name: "Confirm", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "Your share link is ready",
    });
    await expect(dialog.getByLabel("Share link", { exact: true })).toHaveValue(
      replacement,
    );
    await expectDialogFits(dialog);
    await page.screenshot({
      path: testInfo.outputPath("saved-link-dialog.png"),
      animations: "disabled",
    });
    await dialog
      .getByRole("button", { name: "Copy link", exact: true })
      .click();
    await expect.poll(copied).toBe(replacement);
    await preview(replacement, true);
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    await page.reload();
    await expect(
      page.getByText(/Links you already sent still work/),
    ).toHaveCount(0);
    await menu();
    await page
      .getByRole("menuitem", { name: "Copy link", exact: true })
      .click();
    await expect.poll(copied).toBe(replacement);
    expect(rotations).toBe(1);
  });
}

for (const viewport of [
  { width: 320, height: 568 },
  { width: 390, height: 844 },
  { width: 844, height: 390 },
  { width: 1280, height: 720 },
]) {
  test(`create, edit, revoke and delete a share at ${viewport.width}x${viewport.height}`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport);
    const now = new Date("2026-09-21T12:00:00Z");
    await page.clock.setFixedTime(now);
    const sceneTitle = `First scene ${"UnbrokenTitle".repeat(30)}`;
    let shares: GQL.MediaShareFieldsFragment[] = [];
    await page.route("**/api/v3/archive/page-info?**", (route) => {
      const status = new URL(route.request().url()).searchParams.get("state");
      const total = shares.filter(
        (share) =>
          (share.revoked_at === null && new Date(share.expires_at) > now) ===
          (status === "ACTIVE"),
      ).length;
      return route.fulfill({ json: { total, offset: 0 } });
    });
    const inputLog: GQL.MediaShareCreateInput[] = [];
    await page.route("**/graphql", async (route) => {
      const body: unknown = route.request().postDataJSON();
      if (
        typeof body !== "object" ||
        body === null ||
        !("operationName" in body)
      )
        throw new Error("Missing operation");
      const name = body.operationName;
      if (name === "MediaShares") {
        const { status, limit, offset } = z
          .object({
            variables: z.object({
              status: z.enum(GQL.MediaShareStatus),
              limit: z.number(),
              offset: z.number(),
            }),
          })
          .parse(body).variables;
        const data: GQL.MediaSharesQuery = {
          mediaShares: shares
            .filter((share) => {
              const active =
                !share.revoked_at &&
                Date.parse(share.expires_at) > now.getTime();
              return active === (status === GQL.MediaShareStatus.Active);
            })
            .slice(offset, offset + limit),
          sharingConfiguration: {
            __typename: "SharingConfiguration",
            public_url: "https://shares.test/share",
            max_days: 30,
            max_items: 2000,
          },
        };
        await route.fulfill({ json: { data } });
      } else if (name === "ShareTargetSearch") {
        const data: GQL.ShareTargetSearchQuery = {
          findScenes: {
            __typename: "FindScenesResultType",
            scenes: [
              {
                __typename: "Scene",
                id: libraryID(1),
                title: sceneTitle,
                files: [],
              },
            ],
          },
          findImages: {
            __typename: "FindImagesResultType",
            images: [
              {
                __typename: "Image",
                id: libraryID(2),
                title: "Second image",
                visual_files: [],
              },
            ],
          },
          findGalleries: {
            __typename: "FindGalleriesResultType",
            galleries: [],
          },
        };
        await route.fulfill({ json: { data } });
      } else if (name === "MediaShareCreate") {
        // Validate the network boundary before treating the input as GraphQL data.
        const { z } = await import("zod");
        const input = z
          .object({
            variables: z.object({
              input: z.object({
                label: z.string(),
                expires_at: z.iso.datetime(),
                allow_download: z.boolean(),
                show_metadata: z.boolean(),
                targets: z.array(
                  z.object({
                    kind: z.enum(GQL.ShareEntityKind),
                    id: z.string(),
                  }),
                ),
              }),
            }),
          })
          .parse(body).variables.input;
        inputLog.push(input);
        const share: GQL.MediaShareFieldsFragment = {
          __typename: "MediaShare",
          id: "abcdefghijklmnopqrstuv",
          url: `https://shares.test/share/abcdefghijklmnopqrstuv/?key=${"secret".repeat(30)}`,
          label: input.label,
          created_at: now.toISOString(),
          expires_at: input.expires_at,
          revoked_at: null,
          allow_download: input.allow_download,
          show_metadata: input.show_metadata,
          access_count: 0,
          last_accessed_at: null,
          media_count: 2,
          items: input.targets.map((target) => ({
            __typename: "MediaShareEntry",
            ...target,
            title: target.id === libraryID(1) ? sceneTitle : "Second image",
            media_count: 1,
          })),
        };
        shares = [share];
        const data: GQL.MediaShareCreateMutation = {
          mediaShareCreate: {
            __typename: "MediaShareCreated",
            share,
            url: share.url ?? "",
          },
        };
        await route.fulfill({ json: { data } });
      } else if (name === "MediaShareUpdate") {
        const { z } = await import("zod");
        const input = z
          .object({
            variables: z.object({
              input: z.object({
                id: z.string(),
                label: z.string(),
                expires_at: z.iso.datetime(),
                allow_download: z.boolean(),
                show_metadata: z.boolean(),
              }),
            }),
          })
          .parse(body).variables.input;
        const share = shares[0];
        if (!share) throw new Error("Missing share");
        shares = [{ ...share, ...input }];
        await route.fulfill({
          json: { data: { mediaShareUpdate: shares[0] } },
        });
      } else if (name === "MediaShareRevoke") {
        shares = shares.map((share) => ({
          ...share,
          revoked_at: now.toISOString(),
        }));
        await route.fulfill({ json: { data: { mediaShareRevoke: true } } });
      } else if (name === "MediaShareDelete") {
        const { id } = z
          .object({ variables: z.object({ id: z.string() }) })
          .parse(body).variables;
        expect(
          shares.find((share) => share.id === id)?.revoked_at,
        ).toBeTruthy();
        shares = shares.filter((share) => share.id !== id);
        await route.fulfill({ json: { data: { mediaShareDelete: true } } });
      } else throw new Error(`Unexpected operation ${name}`);
    });
    await page.goto("/share-management.html");
    await page
      .getByRole("button", { name: "Create share", exact: true })
      .click();
    const form = page.getByRole("dialog", {
      name: "Create share",
      exact: true,
    });
    await expectDialogFits(form);
    await form.getByPlaceholder("Search scenes, images and galleries…").click();
    await page
      .getByRole("option", { name: `Scene: ${sceneTitle}`, exact: true })
      .click();
    await form.getByPlaceholder("Search scenes, images and galleries…").click();
    await page.getByRole("option", { name: "Image: Second image" }).click();
    await page.keyboard.press("Escape");
    await expectDialogFits(form);

    await form.getByLabel("Time", { exact: true }).fill("");
    await form.getByLabel("Date", { exact: true }).click();
    const calendar = page.locator('[data-slot="popover-content"]');
    await expect(calendar).toBeInViewport({ ratio: 1 });
    await expect(
      calendar.getByRole("button", {
        name: "Sunday, September 20th, 2026",
        exact: true,
      }),
    ).toBeDisabled();
    await calendar
      .getByRole("button", {
        name: "Wednesday, September 23rd, 2026",
        exact: true,
      })
      .click();
    await expect(calendar).toBeHidden();
    await expect(
      form.getByRole("button", { name: "Create share", exact: true }),
    ).toBeDisabled();
    await form.getByLabel("Time", { exact: true }).fill("13:45");
    await expect(
      form.getByRole("button", { name: "Custom", exact: true }),
    ).toHaveAttribute("aria-pressed", "true");
    await expectDialogFits(form);
    await form.locator("form").evaluate((element) => element.scrollTo(0, 0));
    await expectDialogFits(form);
    await form
      .getByRole("button", { name: "Create share", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "Your share link is ready" }),
    ).toBeVisible();
    await expectDialogFits(
      page.getByRole("dialog", { name: "Your share link is ready" }),
    );
    expect(inputLog[0]?.targets).toEqual([
      { kind: GQL.ShareEntityKind.Scene, id: libraryID(1) },
      { kind: GQL.ShareEntityKind.Image, id: libraryID(2) },
    ]);
    expect(inputLog[0]?.allow_download).toBe(false);
    expect(inputLog[0]?.show_metadata).toBe(false);
    expect(inputLog[0]?.expires_at).toBe(
      await page.evaluate(() => new Date("2026-09-23T13:45").toISOString()),
    );
    await page.keyboard.press("Escape");
    await expect(page.getByText("2 media items")).toBeVisible();
    await page.getByRole("button", { name: "Operations", exact: true }).click();
    await page
      .getByRole("menuitem", { name: "Edit share", exact: true })
      .click();
    await expectDialogFits(
      page.getByRole("dialog", { name: "Edit share", exact: true }),
    );
    await page.getByLabel("Label", { exact: true }).fill("Updated share");
    await page.getByRole("button", { name: "7 days", exact: true }).click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Save", exact: true })
      .click();
    await expect(
      page.getByText("Updated share", { exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Operations", exact: true }).click();
    await page
      .getByRole("menuitem", { name: "Revoke share", exact: true })
      .click();
    await page.getByRole("button", { name: "Confirm", exact: true }).click();
    await expect(
      page.getByText("No active shares", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText("Updated share", { exact: true })).toBeHidden();
    await page.getByRole("button", { name: "Inactive", exact: true }).click();
    await expect(page.getByText("Revoked", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Operations", exact: true }).click();
    await page
      .getByRole("menuitem", { name: "Delete share", exact: true })
      .click();
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    expect(shares).toHaveLength(1);
    await page.getByRole("button", { name: "Operations", exact: true }).click();
    await page
      .getByRole("menuitem", { name: "Delete share", exact: true })
      .click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Delete", exact: true })
      .click();
    await expect(
      page.getByText("No inactive shares", { exact: true }),
    ).toBeVisible();
    expect(shares).toEqual([]);
  });
}

test("persists the public share address without delivery controls", async ({
  page,
}) => {
  let configuration: GQL.MediaSharesQuery["sharingConfiguration"] = {
    __typename: "SharingConfiguration",
    public_url: "https://shares.test/share",
    max_days: 30,
    max_items: 2000,
  };
  const updates: GQL.ConfigureSharingMutationVariables[] = [];
  await page.route("**/graphql", async (route) => {
    const request = z
      .object({ operationName: z.string(), variables: z.unknown() })
      .parse(route.request().postDataJSON());
    if (request.operationName === "MediaShares") {
      const data: GQL.MediaSharesQuery = {
        mediaShares: [],
        sharingConfiguration: configuration,
      };
      await route.fulfill({ json: { data } });
    } else if (request.operationName === "ConfigureSharing") {
      const variables = z
        .object({
          public_url: z.string(),
        })
        .parse(request.variables);
      updates.push(variables);
      configuration = { ...configuration, ...variables };
      const data: GQL.ConfigureSharingMutation = {
        configureSharing: configuration,
      };
      await route.fulfill({ json: { data } });
    } else throw new Error(`Unexpected operation ${request.operationName}`);
  });
  await page.goto("/share-management.html");
  const save = page.getByRole("button", { name: "Save", exact: true });
  const address = page.getByRole("textbox", { name: "Public share address" });
  await expect(
    page.getByRole("group", { name: "Media delivery", exact: true }),
  ).toHaveCount(0);
  await expect(save).toBeDisabled();
  await address.fill("https://other.test/share");
  await save.click();
  await expect
    .poll(() => updates)
    .toEqual([{ public_url: "https://other.test/share" }]);
  await expect(save).toBeDisabled();
  await page.reload();
  await expect(address).toHaveValue("https://other.test/share");
  await page.setViewportSize({ width: 320, height: 700 });
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(320);
});

for (const width of [320, 1280]) {
  test(`cleans inactive shares across pages with cancellation and retry at ${width}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 720 });
    const now = new Date("2026-10-18T12:00:00Z");
    await page.clock.setFixedTime(now);
    const active: GQL.MediaShareFieldsFragment = {
      __typename: "MediaShare",
      id: "active-share",
      url: "https://shares.test/share/active-share/?key=example",
      label: "Keep this active share",
      created_at: now.toISOString(),
      expires_at: "2026-10-19T12:00:00Z",
      revoked_at: null,
      allow_download: false,
      show_metadata: false,
      access_count: 0,
      last_accessed_at: null,
      media_count: 1,
      items: [],
    };
    let shares = [
      active,
      ...Array.from({ length: 51 }, (_, index) => ({
        ...active,
        id: `inactive-${index}`,
        label: `Inactive share ${index}`,
        revoked_at: index % 2 === 0 ? now.toISOString() : null,
        expires_at: index % 2 === 0 ? active.expires_at : now.toISOString(),
      })),
    ];
    const isActive = (share: GQL.MediaShareFieldsFragment) =>
      !share.revoked_at && Date.parse(share.expires_at) > now.getTime();
    let cleanupCalls = 0;
    let finishCleanup: (() => void) | undefined;
    await page.route("**/api/v3/archive/page-info?**", (route) => {
      const params = new URL(route.request().url()).searchParams;
      return route.fulfill({
        json: {
          total: shares.filter(
            (share) => isActive(share) === (params.get("state") === "ACTIVE"),
          ).length,
          offset: Number(params.get("cursor") ?? 0),
        },
      });
    });
    await page.route("**/graphql", async (route) => {
      const request = z
        .object({ operationName: z.string(), variables: z.unknown() })
        .parse(route.request().postDataJSON());
      if (request.operationName === "MediaShares") {
        const { status, limit, offset } = z
          .object({
            status: z.enum(GQL.MediaShareStatus),
            limit: z.number(),
            offset: z.number(),
          })
          .parse(request.variables);
        const data: GQL.MediaSharesQuery = {
          mediaShares: shares
            .filter(
              (share) =>
                isActive(share) === (status === GQL.MediaShareStatus.Active),
            )
            .slice(offset, offset + limit),
          sharingConfiguration: {
            __typename: "SharingConfiguration",
            public_url: "https://shares.test/share",
            max_days: 30,
            max_items: 2000,
          },
        };
        return route.fulfill({ json: { data } });
      }
      if (request.operationName !== "MediaSharesDeleteInactive")
        throw new Error(`Unexpected operation ${request.operationName}`);
      cleanupCalls++;
      if (cleanupCalls === 1)
        return route.fulfill({
          json: { errors: [{ message: "Cleanup interrupted" }] },
        });
      await new Promise<void>((resolve) => {
        finishCleanup = resolve;
      });
      const count = shares.filter((share) => !isActive(share)).length;
      shares = shares.filter(isActive);
      const data: GQL.MediaSharesDeleteInactiveMutation = {
        mediaSharesDeleteInactive: count,
      };
      return route.fulfill({ json: { data } });
    });
    await page.goto("/share-management.html");
    const cleanup = page.getByRole("button", {
      name: "Delete all inactive shares",
      exact: true,
    });
    await expect(page.getByText(active.label, { exact: true })).toBeVisible();
    await expect(cleanup).toHaveCount(0);
    await page.getByRole("button", { name: "Inactive", exact: true }).click();
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await expect(
      page.getByText("Inactive share 50", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Inactive share 0", { exact: true }),
    ).toHaveCount(0);
    await cleanup.click();
    const dialog = page.getByRole("dialog", {
      name: "Delete all inactive shares",
      exact: true,
    });
    await expectDialogFits(dialog);
    await expect(dialog.getByText(/across all pages/)).toBeVisible();
    await page.screenshot({
      path: testInfo.outputPath("cleanup-confirmation.png"),
    });
    await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
    expect(cleanupCalls).toBe(0);
    expect(shares).toHaveLength(52);
    await cleanup.click();
    const confirm = dialog.getByRole("button", { name: "Delete", exact: true });
    await confirm.click();
    await expect(
      page.getByText("Cleanup interrupted", { exact: true }),
    ).toBeVisible();
    await expect(dialog).toBeVisible();
    await expect(confirm).toBeEnabled();
    expect(shares).toHaveLength(52);
    await confirm.click();
    await expect(
      dialog.getByRole("button", { name: "Loading Delete", exact: true }),
    ).toBeDisabled();
    await expect(
      dialog.getByRole("button", { name: "Cancel", exact: true }),
    ).toBeDisabled();
    await expect.poll(() => finishCleanup !== undefined).toBe(true);
    finishCleanup?.();
    await expect(dialog).toBeHidden();
    await expect(
      page.getByText("No inactive shares", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Deleted 51 inactive shares", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Previous", exact: true }),
    ).toBeDisabled();
    await expect(cleanup).toBeDisabled();
    expect(cleanupCalls).toBe(2);
    expect(shares).toEqual([active]);
    await page.getByRole("button", { name: "Active", exact: true }).click();
    await expect(page.getByText(active.label, { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Inactive", exact: true }).click();
    await expect(
      page.getByText("No inactive shares", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Inactive share 50", { exact: true }),
    ).toHaveCount(0);
  });
}
