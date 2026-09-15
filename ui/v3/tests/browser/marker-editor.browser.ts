import { test, expect } from "./test";

for (const title of ["", "   ", "  Custom title  "]) {
  test(`creates a marker with ${title === "" ? "an empty" : title.trim() ? "a custom" : "a whitespace-only"} title`, async ({
    page,
  }) => {
    await page.goto("/marker-editor");
    const save = page.getByRole("button", { name: "Save", exact: true });
    const titleInput = page.getByRole("textbox", {
      name: "Title",
      exact: true,
    });
    await expect(titleInput).toHaveAttribute("placeholder", /Optional/);
    await titleInput.fill(title);
    const start = page.getByPlaceholder("Start (hh:mm:ss.ms)");
    await start.fill("0:02");
    await start.blur();
    // A title never substitutes for the required primary tag.
    await expect(save).toBeDisabled();
    await page.getByRole("combobox").first().click();
    await page.getByRole("option", { name: "Example", exact: true }).click();
    await expect(save).toBeEnabled();

    // Optional titles must not weaken timestamp validation.
    await start.fill("");
    await start.blur();
    await expect(save).toBeDisabled();
    await start.fill("0:12");
    await start.blur();
    await expect(save).toBeDisabled();
    await start.fill("0:02");
    await start.blur();
    const end = page.getByPlaceholder("End (hh:mm:ss.ms)");
    await end.fill("0:01");
    await end.blur();
    await expect(save).toBeDisabled();
    await end.fill("0:13");
    await end.blur();
    await expect(save).toBeDisabled();
    await end.fill("0:05");
    await end.blur();
    await expect(save).toBeEnabled();
    await save.click();
    await expect(page.getByTestId("saved-marker-title")).toHaveText(
      title.trim() || "Example",
    );
    expect(await page.evaluate(() => window.markerFixtureSaves)).toEqual([
      {
        operation: "create",
        variables: {
          title: title.trim(),
          seconds: 2,
          end_seconds: 5,
          scene_id: "1",
          primary_tag_id: "tag",
          tag_ids: [],
        },
      },
    ]);
  });
}

test("clearing an existing marker title restores its primary tag label", async ({
  page,
}) => {
  await page.goto("/marker-editor?edit");
  const save = page.getByRole("button", { name: "Save", exact: true });
  await expect(save).toBeDisabled();
  await page.getByRole("textbox", { name: "Title", exact: true }).fill("");
  await expect(save).toBeEnabled();
  await save.click();
  await expect(page.getByTestId("saved-marker-title")).toHaveText("Example");
  expect(await page.evaluate(() => window.markerFixtureSaves)).toEqual([
    {
      operation: "update",
      variables: {
        id: "marker-0",
        title: "",
        seconds: 6,
        end_seconds: 8,
        scene_id: "1",
        primary_tag_id: "tag",
        tag_ids: [],
      },
    },
  ]);
});
