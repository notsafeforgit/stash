import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import {
  collection,
  collectionID,
  mediaRoot,
  rootID,
} from "../fixtures/collections";
import {
  metadataPolicy,
  policyFields,
  policyIDs,
} from "../fixtures/metadata-policy";
import {
  policyDraftInputSchema,
  policyInputSchema,
  type PolicyInput,
  type PolicyDraftInput,
} from "../../src/core/native-archive/metadata-policy-api";

test.use({
  expectedConsoleErrors: [
    "the server responded with a status of 400",
    "the server responded with a status of 409",
    "the server responded with a status of 503",
  ],
});

async function archive(
  page: Page,
  options: {
    loseResponse?: boolean;
    stale?: boolean;
    invalid?: boolean;
    refreshFails?: boolean;
    history?: boolean;
  } = {},
) {
  let current = metadataPolicy();
  const history = options.history
    ? Array.from({ length: 28 }, (_, i) => ({
        ...metadataPolicy(),
        revision: i + 1,
      }))
    : [current];
  if (options.history) current = history[27] ?? current;
  const writes: PolicyInput[] = [];
  const previews: PolicyDraftInput[] = [];
  const requests: URL[] = [];
  let loseResponse = options.loseResponse;
  let stale = options.stale;
  let invalid = options.invalid;
  let refreshFails = false;
  await page.route("**/api/v3/archive/**", async (route) => {
    const url = new URL(route.request().url());
    requests.push(url);
    const path = url.pathname.replace(/^.*\/api\/v3\/archive\//, "");
    if (path === "source-accounts") return route.fulfill({ json: [] });
    if (path === "media-roots") return route.fulfill({ json: [mediaRoot()] });
    if (path === "collections") return route.fulfill({ json: [collection()] });
    if (path === `collections/${collectionID}`)
      return route.fulfill({ json: collection() });
    if (path === `media-roots/${rootID}`)
      return route.fulfill({ json: mediaRoot() });
    if (path === `collections/${collectionID}/metadata-policy`) {
      if (route.request().method() === "PUT") {
        const input = policyInputSchema.parse(route.request().postDataJSON());
        writes.push(input);
        if (invalid) {
          invalid = false;
          return route.fulfill({
            status: 400,
            json: {
              error: "invalid_policy",
              message: "scene.title: invalid jq expression",
            },
          });
        }
        if (stale) {
          stale = false;
          current = {
            ...current,
            revision: current.revision + 1,
            reason: "Concurrent edit",
          };
          history.push(current);
          return route.fulfill({
            status: 409,
            json: { error: "preview_changed" },
          });
        }
        expect(input.expected_revision).toBe(current.revision);
        current = {
          ...current,
          revision: current.revision + 1,
          collection_revision: input.expected_collection_revision,
          definition: input.definition,
          reason: input.reason,
        };
        history.push(current);
        refreshFails = !!options.refreshFails;
        if (loseResponse) {
          loseResponse = false;
          return route.fulfill({ status: 503, json: { error: "unavailable" } });
        }
        return route.fulfill({ json: current });
      }
      if (refreshFails) {
        refreshFails = false;
        return route.fulfill({ status: 503, json: { error: "unavailable" } });
      }
      return route.fulfill({ json: current });
    }
    if (path === `collections/${collectionID}/metadata-policy/history`)
      return route.fulfill({
        json: history
          .filter((row) => row.revision > Number(url.searchParams.get("after")))
          .slice(0, Number(url.searchParams.get("limit"))),
      });
    if (path.startsWith("metadata-fields/"))
      return route.fulfill({ json: policyFields });
    if (path.startsWith("entity-identities/")) {
      const [, kind, local] = path.split("/");
      return route.fulfill({
        json: {
          uuid:
            kind === "performer"
              ? policyIDs.performer
              : kind === "image"
                ? policyIDs.image
                : policyIDs.entity,
          kind,
          local_id: Number(local),
          revision: 1,
        },
      });
    }
    if (path === "metadata-policy/references") {
      const { uuids } = route.request().postDataJSON() as { uuids: string[] };
      return route.fulfill({
        json: uuids.map((uuid) => ({
          requested_uuid: uuid,
          entity: { uuid, kind: "performer", revision: 1, local_id: 10 },
          name: "River",
          disambiguation: "",
        })),
      });
    }
    if (path.includes("/samples/") && path.endsWith("/files"))
      return route.fulfill({
        json: [
          {
            file_uuid: policyIDs.file,
            relative_path: "Purchased/River/sample.mp4",
          },
        ],
      });
    if (path.includes("/samples/") && path.endsWith("/sources"))
      return route.fulfill({
        json: [
          {
            capture_uuid: policyIDs.capture,
            attachment_uuid: policyIDs.attachment,
            post_uuid: policyIDs.post,
            title: "Source title",
            platform: "reddit",
            origin: "gallery-dl",
            captured_at: "2026-10-05T12:00:00Z",
          },
        ],
      });
    if (path === "metadata-policy/draft-preview") {
      const input = policyDraftInputSchema.parse(
        route.request().postDataJSON(),
      );
      previews.push(input);
      return route.fulfill({
        json: {
          context: {
            collection_uuid: input.collection_uuid,
            collection_revision: input.expected_collection_revision,
            policy_revision: input.expected_policy_revision,
            entity_uuid: input.entity_uuid,
            expected_entity_revision: 1,
            relative_path: "Purchased/River/sample.mp4",
            created: input.event === "create",
            source: input.source,
          },
          state: "ready",
          data: {
            entity: { title: "" },
            source: { metadata: { title: "Source title" } },
            context: {
              created: input.event === "create",
              filename: "sample.mp4",
              relative_path: "Purchased/River/sample.mp4",
            },
          },
          changes: [
            {
              field: "title",
              status: "ready",
              current: "",
              value: "Source title",
            },
          ],
        },
      });
    }
    throw new Error(`Unexpected policy request ${path}`);
  });
  return { writes, previews, requests };
}

async function open(page: Page) {
  await page.goto(`/collections?collection=${collectionID}`);
  await page
    .getByRole("button", { name: "Edit metadata rules", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Save metadata policy", exact: true }),
  ).toBeEnabled();
}

test("edits plain jq and tests selected scene and image data without mutations", async ({
  page,
}) => {
  const remote = await archive(page);
  await open(page);
  const expression = page.getByRole("textbox", {
    name: "jq expression",
    exact: true,
  });
  await expression.fill('.source.metadata.title // "Fallback"');
  await page
    .getByRole("button", { name: "Test with a scene or image", exact: true })
    .click();
  const search = page.getByRole("combobox", {
    name: "Scene or image",
    exact: true,
  });
  await search.click();
  await search.fill("Sample");
  await page
    .getByRole("option", { name: "Sample video (#7)", exact: true })
    .click();
  await page
    .getByRole("combobox", { name: "Source capture", exact: true })
    .click();
  await page.getByRole("option", { name: /^Source title/ }).click();
  await page
    .getByRole("button", { name: "Test draft mappings", exact: true })
    .click();
  await expect(
    page.getByText("Preview only — nothing was changed", { exact: true }),
  ).toBeVisible();
  expect(remote.previews[0]).toMatchObject({
    event: "existing",
    source: { capture_uuid: policyIDs.capture },
  });
  expect(remote.writes).toEqual([]);
  await page
    .getByRole("group", { name: "Event to simulate", exact: true })
    .getByRole("button", { name: "New item", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Test draft mappings", exact: true })
    .click();
  await expect.poll(() => remote.previews.length).toBe(2);
  expect(remote.previews[1]?.event).toBe("create");
  await expression.fill(".source.metadata.title | ascii_upcase");
  await expect(
    page.getByText("Preview only — nothing was changed", { exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("group", { name: "Sample type", exact: true })
    .getByRole("button", { name: "Images", exact: true })
    .click();
  await search.click();
  await search.fill("Sample");
  await page
    .getByRole("option", { name: "Sample image (#8)", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Test draft mappings", exact: true })
    .click();
  await expect.poll(() => remote.previews.length).toBe(3);
  expect(remote.previews[2]?.entity_uuid).toBe(policyIDs.image);
  expect(remote.writes).toEqual([]);
  await page
    .getByRole("button", { name: "Sample data for jq", exact: true })
    .click();
  await expect(
    page.locator("pre").filter({ hasText: '"filename": "sample.mp4"' }),
  ).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
  await page.screenshot({
    path: test.info().outputPath("metadata-policy-mobile.png"),
    fullPage: true,
  });
});

test("selects a fixed performer for folder scans and records schema-constrained rules", async ({
  page,
}) => {
  const remote = await archive(page);
  await open(page);
  await page
    .getByRole("button", { name: "Add field mapping", exact: true })
    .click();
  const performer = page.getByRole("group", {
    name: "Performers",
    exact: true,
  });
  await performer
    .getByRole("button", { name: "Fixed value", exact: true })
    .click();
  const search = performer.getByRole("combobox", {
    name: /Choose a library entry/,
  });
  await search.click();
  await search.fill("River");
  await page.getByRole("option", { name: "River (#10)", exact: true }).click();
  await expect(
    performer.getByText("River (#10)", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("switch", {
      name: "Mark organized after metadata is selected",
      exact: true,
    })
    .check();
  const required = page.getByRole("group", {
    name: "Required metadata",
    exact: true,
  });
  await required.getByRole("checkbox", { name: "Title", exact: true }).check();
  await required
    .getByRole("checkbox", { name: "Performers", exact: true })
    .check();
  await page
    .getByRole("button", { name: "Save metadata policy", exact: true })
    .click();
  await expect(
    page.getByText("Metadata policy saved", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(1);
  expect(
    remote.writes[0]?.definition.rules?.scene?.mappings?.performers,
  ).toEqual({ value: [policyIDs.performer] });
  expect(remote.writes[0]?.definition.rules?.scene?.organized_requires).toEqual(
    ["title", "performers"],
  );
  await expect(
    required.getByRole("checkbox", { name: "Title", exact: true }),
  ).toBeChecked();
  await expect(
    required.getByRole("checkbox", { name: "Performers", exact: true }),
  ).toBeChecked();
  await page
    .getByRole("switch", {
      name: "Mark organized after metadata is selected",
      exact: true,
    })
    .uncheck();
  await expect(
    required.getByRole("checkbox", { name: "Title", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("switch", {
      name: "Mark organized after metadata is selected",
      exact: true,
    })
    .check();
  await expect(
    required.getByRole("checkbox", { name: "Title", exact: true }),
  ).toBeEnabled();
  expect(remote.previews).toEqual([]);
  await page.screenshot({
    path: test.info().outputPath("metadata-completeness-mobile.png"),
    fullPage: true,
    animations: "disabled",
  });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.screenshot({
    path: test.info().outputPath("metadata-policy-desktop.png"),
    fullPage: true,
    animations: "disabled",
  });
});

test("recovers a committed policy save after reload without sending another PUT", async ({
  page,
}) => {
  const remote = await archive(page, { loseResponse: true });
  await open(page);
  await page
    .getByRole("textbox", { name: "jq expression", exact: true })
    .fill('"Recovered"');
  await page
    .getByRole("button", { name: "Save metadata policy", exact: true })
    .click();
  await expect(
    page.getByText("Could not complete this step", { exact: true }),
  ).toBeVisible();
  await expect.poll(() => remote.writes.length).toBe(1);
  await page.reload();
  await page
    .getByRole("button", { name: "Edit metadata rules", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Check and retry policy change", exact: true })
    .click();
  await expect(
    page.getByText("Metadata policy saved", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(1);
});

test("keeps invalid drafts for correction and shows the server validation detail", async ({
  page,
}) => {
  const remote = await archive(page, { invalid: true });
  await open(page);
  await page
    .getByRole("textbox", { name: "jq expression", exact: true })
    .fill(".broken[");
  await page
    .getByRole("button", { name: "Save metadata policy", exact: true })
    .click();
  await expect(
    page.getByText("scene.title: invalid jq expression", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Review and edit draft", exact: true })
    .click();
  const expression = page.getByRole("textbox", {
    name: "jq expression",
    exact: true,
  });
  await expect(expression).toBeEnabled();
  await expect(expression).toHaveValue(".broken[");
  await expression.fill('"Corrected"');
  await page
    .getByRole("button", { name: "Save metadata policy", exact: true })
    .click();
  await expect(
    page.getByText("Metadata policy saved", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(2);
});

test("requires review after concurrent policy edits and keeps a confirmed save after refresh failure", async ({
  page,
}) => {
  const remote = await archive(page, { stale: true, refreshFails: true });
  await open(page);
  await page
    .getByRole("textbox", { name: "jq expression", exact: true })
    .fill('"My title"');
  await page
    .getByRole("button", { name: "Save metadata policy", exact: true })
    .click();
  await expect(
    page.getByText("Review the rejected draft", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Review and edit draft", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Save metadata policy", exact: true }),
  ).toBeEnabled();
  await page
    .getByRole("button", { name: "Save metadata policy", exact: true })
    .click();
  await expect(
    page.getByText(
      "The policy is saved, but this view could not be refreshed",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(
    page.getByText("Metadata policy saved", { exact: true }),
  ).toBeVisible();
  expect(remote.writes.map((row) => row.expected_revision)).toEqual([1, 2]);
});

test("loads immutable policy history in bounded pages only when expanded", async ({
  page,
}) => {
  const remote = await archive(page, { history: true });
  await open(page);
  expect(remote.requests.some((url) => url.pathname.endsWith("/history"))).toBe(
    false,
  );
  await page
    .getByRole("button", { name: "Metadata policy history", exact: true })
    .click();
  await expect(
    page.getByText("Policy revision 25 · collection revision 1", {
      exact: true,
    }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    page.getByText("Policy revision 28 · collection revision 1", {
      exact: true,
    }),
  ).toBeVisible();
  expect(
    remote.requests
      .filter((url) => url.pathname.endsWith("/history"))
      .map((url) => [
        url.searchParams.get("after"),
        url.searchParams.get("limit"),
      ]),
  ).toEqual([
    ["0", "25"],
    ["25", "25"],
  ]);
});
