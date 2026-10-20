import { test, expect } from "@playwright/test";

test("filtered bulk edits monitor UUID jobs, refresh the list and retain outcomes after restart", async ({
  page,
  request,
}, testInfo) => {
  const prefix = `bulkstack${testInfo.project.name}`;
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
  for (const suffix of ["A", "B", "C"]) {
    await query(
      `mutation { performerCreate(input:{name:"${prefix} ${suffix}"}) { id } }`,
    );
  }
  await page.goto(`/mounted/performers?q=${prefix}&perPage=2`);
  await page.getByLabel("Username", { exact: true }).fill("owner");
  await page.getByLabel("Password", { exact: true }).fill("stack +& password");
  await page.getByRole("button", { name: "Login", exact: true }).click();
  const cards = page.locator("article.performer-card");
  await expect(cards).toHaveCount(2);
  await cards.first().click({ button: "right" });
  await page.getByRole("menuitem", { name: "Select All", exact: true }).click();
  await cards.first().click({ button: "right" });
  await page.getByRole("menuitem", { name: /Edit 2 performers/ }).click();
  const sheet = page.getByRole("dialog", { name: "Edit 2 performers" });
  await sheet.getByRole("switch", { name: "Apply to all 3 matching" }).click();
  const country = sheet
    .locator('[data-slot="field"]')
    .filter({ has: page.getByText("Country", { exact: true }) });
  await country.getByRole("button", { name: "Set", exact: true }).click();
  await country.getByRole("textbox").fill("NZ");
  const monitored: string[] = [];
  page.on("request", (request) => {
    if (request.method() !== "POST" || !request.url().endsWith("/graphql"))
      return;
    const body = request.postDataJSON();
    if (body.operationName === "FindJob")
      monitored.push(body.variables.input.id);
  });
  const accepted = page.waitForResponse((response) => {
    const request = response.request();
    return (
      request.method() === "POST" &&
      request.url().endsWith("/graphql") &&
      request.postDataJSON().operationName === "BulkPerformerUpdate"
    );
  });
  await sheet.getByRole("button", { name: "Save", exact: true }).click();
  const response = await (await accepted).json();
  expect(response.errors).toBeUndefined();
  const result = response.data.bulkPerformerUpdate;
  expect(result).toMatchObject({
    status: "QUEUED",
    selected_count: 3,
    updated_ids: [],
  });
  expect(result.job_id).toMatch(/^[0-9a-f-]{36}$/);
  await expect(sheet).toBeHidden();
  await expect.poll(() => monitored).toContain(result.job_id);
  await expect(cards.first()).toContainText("New Zealand");
  const all = await query(
    `{ findPerformers(filter:{q:"${prefix}",per_page:-1}) { performers { country } } }`,
  );
  expect(all.findPerformers.performers).toEqual([
    { country: "NZ" },
    { country: "NZ" },
    { country: "NZ" },
  ]);
  expect((await request.post("/__test/restart")).ok()).toBe(true);
  const retained = await query(
    `{ findJob(input:{id:"${result.job_id}"}) { id status } }`,
  );
  expect(retained.findJob).toEqual({
    id: result.job_id,
    status: "FINISHED",
  });
});

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
