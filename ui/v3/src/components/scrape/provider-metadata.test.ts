import { FormApi } from "@tanstack/react-form";
import { expect, it } from "vitest";
import {
  ProviderMetadataDraft,
  providerPatch,
  providerCreationFields,
} from "./provider-metadata";
import type { ScrapeSource } from "./use-available-scrapers";

const source: ScrapeSource = {
  kind: "stashBox",
  endpoint: "https://provider.test/graphql",
  name: "Provider",
};
const second: ScrapeSource = {
  ...source,
  endpoint: "https://second.test/graphql",
};

it("retains choices through form Apply but permanently drops later manual edits", () => {
  const form = new FormApi({
    defaultValues: { title: "Old", code: "", organized: false },
  });
  const unmount = form.mount();
  const draft = new ProviderMetadataDraft();
  const subscription = form.store.subscribe(() =>
    draft.observe(form.state.values),
  );
  const patch = { title: "Imported", code: "REMOTE", organized: true };
  const selection = providerPatch(
    "scene",
    source,
    "scene-1",
    patch,
    () => "overwrite",
  );
  draft.apply(patch, selection, () => {
    form.setFieldValue("title", patch.title);
    form.setFieldValue("code", patch.code);
    form.setFieldValue("organized", true);
  });
  expect(draft.selections(form.state.values)).toEqual([
    {
      endpoint: source.endpoint,
      remote_id: "scene-1",
      fields: ["code", "title"],
    },
  ]);
  form.setFieldValue("title", "Manual");
  form.setFieldValue("title", "Imported");
  expect(draft.selections(form.state.values)[0]?.fields).toEqual(["code"]);
  draft.clear();
  form.reset();
  expect(draft.selections(form.state.values)).toEqual([]);
  subscription.unsubscribe();
  unmount();
});

it("keeps multiple sources for explicitly merged values and replaces scalar attribution", () => {
  const draft = new ProviderMetadataDraft();
  const first = {
    name: "First",
    urls: ["https://first.test/"],
    height_cm: "168",
    stash_ids: [],
    favorite: true,
  };
  draft.apply(
    first,
    providerPatch("performer", source, "performer-1", first, () => "merge"),
    () => {},
  );
  const next = {
    name: "Second",
    urls: [...first.urls, "https://second.test/"],
  };
  draft.apply(
    next,
    providerPatch("performer", second, "performer-2", next, () => "merge"),
    () => {},
  );
  expect(draft.selections({ ...first, ...next })).toEqual([
    {
      endpoint: source.endpoint,
      remote_id: "performer-1",
      fields: ["height", "urls"],
    },
    {
      endpoint: second.endpoint,
      remote_id: "performer-2",
      fields: ["name", "urls"],
    },
  ]);
  const overwritten = { urls: ["https://third.test/"] };
  draft.apply(
    overwritten,
    providerPatch(
      "performer",
      second,
      "performer-2",
      overwritten,
      () => "overwrite",
    ),
    () => {},
  );
  expect(
    draft.selections({ ...first, ...next, ...overwritten })[0]?.fields,
  ).toEqual(["height"]);
  // An ordinary scrape replaces attribution on just its own fields.
  draft.apply({ height_cm: "170" }, undefined, () => {});
  expect(
    draft.selections({ ...next, ...overwritten, height_cm: "170" }),
  ).toEqual([
    {
      endpoint: second.endpoint,
      remote_id: "performer-2",
      fields: ["name", "urls"],
    },
  ]);
});

it("attributes related creations using the item's remote ID and only accepted names", () => {
  expect(
    providerCreationFields(
      source,
      { name: "Tag", remote_site_id: "tag-7" },
      "Tag",
    ),
  ).toEqual({
    stash_ids: [{ endpoint: source.endpoint, stash_id: "tag-7" }],
    provider_metadata: [
      { endpoint: source.endpoint, remote_id: "tag-7", fields: ["name"] },
    ],
  });
  expect(
    providerCreationFields(
      source,
      { name: "Tag", remote_site_id: "tag-7" },
      "Custom name",
    ).provider_metadata,
  ).toEqual([]);
  expect(() => providerCreationFields(source, { name: "Tag" }, "Tag")).toThrow(
    "missing_provider_remote_id",
  );
  expect(providerCreationFields(undefined, { name: "Tag" }, "Tag")).toEqual({});
  expect(() =>
    providerPatch("scene", source, null, { title: "Title" }, () => "overwrite"),
  ).toThrow("missing_provider_remote_id");
});
