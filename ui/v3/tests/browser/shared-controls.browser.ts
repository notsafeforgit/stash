import { test, expect } from "./test";

test("shared folder controls expose names, state and native keyboard activation", async ({
  page,
}) => {
  await page.goto("/shared-controls.html");
  const expand = page.getByRole("button", {
    name: "Expand Library",
    exact: true,
  });
  await expect(expand).toHaveAttribute("aria-expanded", "false");
  await expand.focus();
  await page.keyboard.press("Space");
  const collapse = page.getByRole("button", {
    name: "Collapse Library",
    exact: true,
  });
  await expect(collapse).toHaveAttribute("aria-expanded", "true");
  await expect(
    page.getByRole("button", { name: "Nested", exact: true }),
  ).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(expand).toHaveAttribute("aria-expanded", "false");
  await expect(
    page.getByRole("button", { name: "Nested", exact: true }),
  ).toHaveCount(0);

  await page.getByRole("button", { name: "Clear", exact: true }).focus();
  await page.keyboard.press("Enter");
  const input = page.getByRole("textbox", { name: "Search folders" });
  await expect(input).toHaveValue("");
  await expect(input).toBeFocused();

  const pin = page.getByRole("button", { name: "Pin", exact: true });
  await expect(pin).toHaveAttribute("aria-pressed", "false");
  await pin.focus();
  await page.keyboard.press("Space");
  await expect(
    page.getByRole("button", { name: "Unpin", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");

  await page.getByRole("button", { name: "Go to next page" }).focus();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("button", { name: "Page 2", exact: true }),
  ).toHaveAttribute("aria-current", "page");
  await expect(
    page.getByRole("button", { name: "Go to next page" }),
  ).toBeDisabled();
});
