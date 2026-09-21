import * as GQL from "../../src/core/generated-graphql";
import { test, expect } from "./test";
import { z } from "zod";

test("create, edit, revoke and delete a share through the active and inactive views", async ({
  page,
}) => {
  let shares: GQL.MediaShareFieldsFragment[] = [];
  const inputLog: GQL.MediaShareCreateInput[] = [];
  await page.route("**/graphql", async (route) => {
    const body: unknown = route.request().postDataJSON();
    if (typeof body !== "object" || body === null || !("operationName" in body))
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
              !share.revoked_at && Date.parse(share.expires_at) > Date.now();
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
            { __typename: "Scene", id: "1", title: "First scene", files: [] },
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
        findGalleries: { __typename: "FindGalleriesResultType", galleries: [] },
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
                z.object({ kind: z.enum(GQL.ShareEntityKind), id: z.string() }),
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
        created_at: new Date().toISOString(),
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
          title: target.id === "1" ? "First scene" : "Second image",
          media_count: 1,
        })),
      };
      shares = [share];
      const data: GQL.MediaShareCreateMutation = {
        mediaShareCreate: {
          __typename: "MediaShareCreated",
          share,
          url: `https://shares.test/share/${share.id}/#secret`,
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
      await route.fulfill({ json: { data: { mediaShareUpdate: shares[0] } } });
    } else if (name === "MediaShareRevoke") {
      shares = shares.map((share) => ({
        ...share,
        revoked_at: new Date().toISOString(),
      }));
      await route.fulfill({ json: { data: { mediaShareRevoke: true } } });
    } else if (name === "MediaShareDelete") {
      const { id } = z
        .object({ variables: z.object({ id: z.string() }) })
        .parse(body).variables;
      expect(shares.find((share) => share.id === id)?.revoked_at).toBeTruthy();
      shares = shares.filter((share) => share.id !== id);
      await route.fulfill({ json: { data: { mediaShareDelete: true } } });
    } else throw new Error(`Unexpected operation ${name}`);
  });
  await page.goto("/share-management.html");
  await page.getByRole("button", { name: "Create share", exact: true }).click();
  const form = page.getByRole("dialog");
  await form.getByPlaceholder("Search scenes, images and galleries…").click();
  await page.getByRole("option", { name: "Scene: First scene" }).click();
  await form.getByPlaceholder("Search scenes, images and galleries…").click();
  await page.getByRole("option", { name: "Image: Second image" }).click();
  await page.keyboard.press("Escape");
  await form.getByRole("button", { name: "Create share", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Your share link is ready" }),
  ).toBeVisible();
  expect(inputLog[0]?.targets).toEqual([
    { kind: GQL.ShareEntityKind.Scene, id: "1" },
    { kind: GQL.ShareEntityKind.Image, id: "2" },
  ]);
  expect(inputLog[0]?.allow_download).toBe(false);
  expect(inputLog[0]?.show_metadata).toBe(false);
  await page.keyboard.press("Escape");
  await expect(page.getByText("2 media items")).toBeVisible();
  await page.getByRole("button", { name: "Operations", exact: true }).click();
  await page.getByRole("menuitem", { name: "Edit share", exact: true }).click();
  await page.getByLabel("Label", { exact: true }).fill("Updated share");
  await page.getByRole("button", { name: "7 days", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Save", exact: true })
    .click();
  await expect(page.getByText("Updated share", { exact: true })).toBeVisible();
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
