import { test, expect } from "@playwright/test";

test("performer merge uses UUIDs for names, photos, tags and the surviving route", async ({
  page,
  request,
}, testInfo) => {
  const prefix = `uuidmerge${testInfo.project.name}`;
  const query = async (
    query: string,
    variables: Record<string, unknown> = {},
  ) => {
    const response = await request.post("/mounted/graphql", {
      headers: { ApiKey: "stack-test-api-key" },
      data: { query, variables },
    });
    expect(response.ok()).toBe(true);
    const result = await response.json();
    expect(result.errors).toBeUndefined();
    return result.data;
  };
  // A valid 1x1 image in the application's canonical stored format. The
  // browser stack need not install FFmpeg to exercise a byte-preserving copy.
  const photo = Buffer.from(
    "UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA",
    "base64",
  );
  const { performerCreate: source } = await query(
    `mutation($input:PerformerCreateInput!){performerCreate(input:$input){id name image_path}}`,
    {
      input: {
        name: `${prefix} source`,
        image_input: {
          data: `data:image/webp;base64,${photo.toString("base64")}`,
        },
      },
    },
  );
  const { performerCreate: destination } = await query(
    `mutation($name:String!){performerCreate(input:{name:$name}){id name}}`,
    { name: `${prefix} destination` },
  );
  const originalPhoto = await request.get(source.image_path, {
    headers: { ApiKey: "stack-test-api-key" },
  });
  expect(originalPhoto.ok()).toBe(true);
  const { tagCreate: tag } = await query(
    `mutation($name:String!){tagCreate(input:{name:$name}){id}}`,
    { name: `${prefix} tag` },
  );
  await query(
    `mutation($id:ID!,$tag:ID!){performerUpdate(input:{id:$id,tag_ids:[$tag]}){id}}`,
    { id: source.id, tag: tag.id },
  );
  await page.goto(`/mounted/performers?q=${prefix}`);
  await page.getByLabel("Username", { exact: true }).fill("owner");
  await page.getByLabel("Password", { exact: true }).fill("stack +& password");
  await page.getByRole("button", { name: "Login", exact: true }).click();
  await page
    .locator("article.performer-card")
    .filter({ hasText: source.name })
    .click({ button: "right" });
  await page.getByRole("menuitem", { name: "Merge into…" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("combobox").click();
  await dialog.getByRole("combobox").fill(destination.name);
  await page
    .getByRole("option", { name: destination.name, exact: true })
    .click();
  await dialog
    .locator("#merge-row-name")
    .getByRole("button", { name: /^Source/ })
    .click();
  await dialog
    .locator("#merge-row-image_path")
    .getByRole("button", { name: /^Source/ })
    .click();
  const mergedResponse = page.waitForResponse((response) => {
    const req = response.request();
    return (
      req.method() === "POST" &&
      req.url().endsWith("/graphql") &&
      req.postDataJSON().operationName === "PerformerMerge"
    );
  });
  await dialog.getByRole("button", { name: "Merge", exact: true }).click();
  const response = await mergedResponse;
  const body = response.request().postDataJSON();
  expect(body.variables.input).toMatchObject({
    source: [source.id],
    destination: destination.id,
    values: {
      id: destination.id,
      name: source.name,
      image_input: { performer_id: source.id },
      tag_ids: [tag.id],
    },
  });
  expect((await response.json()).errors).toBeUndefined();
  await expect(dialog).toBeHidden();
  await expect(page).toHaveURL(
    new RegExp(`/mounted/performers/${destination.id}`),
  );
  const result = await query(
    `query($old:ID!,$id:ID!){old:findPerformer(id:$old){id} current:findPerformer(id:$id){id name image_path tags{id} aliases{alias}}}`,
    { old: source.id, id: destination.id },
  );
  expect(result.old.id).toBe(destination.id);
  expect(result.current).toMatchObject({
    id: destination.id,
    name: source.name,
    tags: [{ id: tag.id }],
    aliases: [{ alias: destination.name }],
  });
  const copiedPhoto = await request.get(result.current.image_path, {
    headers: { ApiKey: "stack-test-api-key" },
  });
  expect(copiedPhoto.ok()).toBe(true);
  expect(await copiedPhoto.body()).toEqual(await originalPhoto.body());
});
