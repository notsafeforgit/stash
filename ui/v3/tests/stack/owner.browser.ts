import { test, expect } from "@playwright/test";

test("owner login, failed save, expired session and durable jobs through a subpath proxy", async ({
  page,
  context,
  request,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/mounted/settings/security");
  await expect(page).toHaveURL(/\/mounted\/login\?/);
  await page.getByLabel("Username", { exact: true }).fill("owner");
  await page.getByLabel("Password", { exact: true }).fill("incorrect");
  await page.getByRole("button", { name: "Login", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "Invalid username or password",
  );
  await page.getByLabel("Password", { exact: true }).fill("stack +& password");
  await page.getByRole("button", { name: "Login", exact: true }).click();
  await expect(page).toHaveURL(/\/mounted\/settings\/security$/);
  await page.getByRole("button", { name: "Edit…", exact: true }).click();
  const credentials = page.getByRole("dialog", {
    name: "Credentials",
    exact: true,
    includeHidden: true,
  });
  const password = credentials.getByLabel("Password", { exact: true });
  await password.fill("stack +& password");
  expect((await request.post("/__test/block-config")).ok()).toBe(true);
  try {
    await credentials
      .getByRole("button", { name: "Save", exact: true })
      .click();
    await expect(credentials.getByRole("alert")).toContainText(
      "Could not confirm that credentials were saved",
    );
    await expect(password).toHaveValue("stack +& password");
  } finally {
    expect((await request.post("/__test/restore-config")).ok()).toBe(true);
  }
  await context.clearCookies();
  await credentials.getByRole("button", { name: "Save", exact: true }).click();
  const recovery = page.getByRole("dialog", {
    name: "Sign in to continue",
    exact: true,
  });
  await expect(recovery).toBeVisible();
  await expect(password).toHaveValue("stack +& password");
  await recovery.getByLabel("Username", { exact: true }).fill("owner");
  await recovery
    .getByLabel("Password", { exact: true })
    .fill("stack +& password");
  await recovery.getByRole("button", { name: "Login", exact: true }).click();
  await expect(recovery).toBeHidden();
  await expect(credentials).toBeHidden();
  await expect(page).toHaveURL(/\/mounted\/settings\/security$/);

  const query = async (document: string) => {
    const response = await request.post("/mounted/graphql", {
      headers: { ApiKey: "stack-test-api-key" },
      data: { query: document },
    });
    expect(response.ok()).toBe(true);
    const result = await response.json();
    expect(result.errors).toBeUndefined();
    return result.data;
  };
  const settings = await query(
    "{ configuration { general { username passwordConfigured } } }",
  );
  expect(settings.configuration.general).toEqual({
    username: "owner",
    passwordConfigured: true,
  });
  const { optimiseDatabase: id } = await query("mutation { optimiseDatabase }");
  expect(id).toMatch(/^[0-9a-f-]{36}$/);
  await expect
    .poll(
      async () =>
        (await query(`{ findJob(input:{id:"${id}"}) { status } }`)).findJob
          .status,
    )
    .toBe("FINISHED");
  expect((await request.post("/__test/restart")).ok()).toBe(true);
  const result = await query(
    `{ findJob(input:{id:"${id}"}) { id status } jobHistory(limit:100) { id } }`,
  );
  expect(result.findJob).toEqual({ id, status: "FINISHED" });
  expect(result.jobHistory).toContainEqual({ id });
  await page.goto("/mounted/settings/tasks");
  await expect(
    page.getByRole("heading", { name: "Recent library tasks" }),
  ).toBeVisible();
  await expect(
    page.getByText("Completed", { exact: true }).first(),
  ).toBeVisible();
  expect(errors).toEqual([]);
});
