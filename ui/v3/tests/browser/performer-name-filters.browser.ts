import { test, expect } from "./test";

test("performer name picker offers Names and Canonical name on mobile", async ({
  page,
}) => {
  await page.goto("/performer-list");
  await page.getByRole("button", { name: "Filters", exact: true }).tap();
  await page.getByRole("button", { name: "Add first condition" }).tap();
  const condition = page.locator(".filter-condition-shell");
  const field = condition.getByRole("combobox").first();
  await field.tap();
  await field.fill("name");
  await expect(page.getByRole("option")).toHaveCount(2);
  await expect(page.getByRole("option", { name: /^Names\b/ })).toBeVisible();
  await expect(
    page.getByRole("option", { name: /^Canonical name\b/ }),
  ).toBeVisible();
  await page.screenshot({
    path: "/tmp/stash-performer-name-filters.png",
    animations: "disabled",
  });
  await page.getByRole("option", { name: /^Names\b/ }).tap();
  await condition.getByRole("textbox").fill("River");
  await expect(page).toHaveURL(/fa=/);
  await page.reload();
  await page.getByRole("button", { name: "Filters", exact: true }).tap();
  await expect(field).toHaveValue("Names");
  await expect(condition.getByRole("textbox")).toHaveValue("River");
  await field.tap();
  await field.fill("Canonical");
  await page.getByRole("option", { name: /^Canonical name\b/ }).tap();
  await condition.getByRole("textbox").fill("River");
  await expect(field).toHaveValue("Canonical name");
  await field.tap();
  await field.fill("alias");
  await expect(page.getByRole("option")).toHaveCount(0);
});

test("saved noncanonical-name conditions remain labeled and editable without offering them as new fields", async ({
  page,
}) => {
  const ast = { k: 1, f: "aliases", m: 0, v: "River", cs: true };
  const fa = `u.${Buffer.from(JSON.stringify(ast)).toString("base64url")}`;
  await page.addInitScript(() => {
    localStorage.setItem(
      "filter-builder-pinned-fields:PERFORMERS",
      JSON.stringify(["aliases"]),
    );
  });
  await page.goto(`/performer-list?fa=${fa}`);
  await page.getByRole("button", { name: "Filters", exact: true }).tap();
  const condition = page.locator(".filter-condition-shell");
  const field = condition.getByRole("combobox").first();
  await expect(field).toHaveValue("Other names");
  await expect(condition.getByRole("textbox")).toHaveValue("River");
  await expect(
    condition.getByRole("switch", { name: "Case sensitive" }),
  ).toBeChecked();
  await condition.getByRole("textbox").fill("River two");
  await expect(page).not.toHaveURL(`/performer-list?fa=${fa}`);
  await page.reload();
  await page.getByRole("button", { name: "Filters", exact: true }).tap();
  await expect(field).toHaveValue("Other names");
  await expect(condition.getByRole("textbox")).toHaveValue("River two");
  await field.tap();
  await field.fill("names");
  await expect(page.getByRole("option")).toHaveCount(1);
  await page.getByRole("option", { name: /^Names\b/ }).tap();
  await expect(field).toHaveValue("Names");
});
