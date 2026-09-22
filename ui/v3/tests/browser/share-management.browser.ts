import * as GQL from "../../src/core/generated-graphql";
import { test, expect } from "./test";
import { z } from "zod";
import type { Locator } from "@playwright/test";

test.use({ timezoneId: "America/Los_Angeles" });

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
          use_existing_previews: false,
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
              id: "1",
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
              id: "2",
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
              id: "3",
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
              id: "4",
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
              id: "5",
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
            use_existing_previews: false,
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
              { __typename: "Scene", id: "1", title: sceneTitle, files: [] },
            ],
          },
          findImages: {
            __typename: "FindImagesResultType",
            images: [
              {
                __typename: "Image",
                id: "2",
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
            title: target.id === "1" ? sceneTitle : "Second image",
            media_count: 1,
          })),
        };
        shares = [share];
        const data: GQL.MediaShareCreateMutation = {
          mediaShareCreate: {
            __typename: "MediaShareCreated",
            share,
            url: `https://shares.test/share/${share.id}/#${"secret".repeat(30)}`,
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
      { kind: GQL.ShareEntityKind.Scene, id: "1" },
      { kind: GQL.ShareEntityKind.Image, id: "2" },
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

test("persists instance-wide preview reuse independently of share permissions", async ({
  page,
}) => {
  let configuration: GQL.MediaSharesQuery["sharingConfiguration"] = {
    __typename: "SharingConfiguration",
    public_url: "https://shares.test/share",
    use_existing_previews: false,
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
        .object({ public_url: z.string(), use_existing_previews: z.boolean() })
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
  const toggle = page.getByRole("switch", { name: "Use existing previews" });
  const save = page.getByRole("button", { name: "Save", exact: true });
  await expect(toggle).not.toBeChecked();
  await expect(save).toBeDisabled();
  await toggle.click();
  await save.click();
  await expect
    .poll(() => updates)
    .toEqual([
      { public_url: "https://shares.test/share", use_existing_previews: true },
    ]);
  await expect(save).toBeDisabled();
  await page.reload();
  await expect(toggle).toBeChecked();
  await expect(page.getByText(/Embedded metadata is retained/)).toBeVisible();
  await toggle.click();
  await save.click();
  await expect.poll(() => updates.at(-1)?.use_existing_previews).toBe(false);
  await expect(save).toBeDisabled();
});
