// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import {
  ApolloClient,
  ApolloLink,
  InMemoryCache,
  Observable,
} from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { afterEach, expect, it, vi } from "vitest";
import {
  PluginSettingEditorV3,
  PluginSettingTypeV3,
  PluginPreviewEntityV3,
  type PluginMappingTargetV3,
} from "@/core/generated-graphql";
import { PluginSettingsForm } from "./plugin-settings-form";

let cleanup = async () => {};
afterEach(async () => {
  await cleanup();
  vi.unstubAllGlobals();
});

async function fixture(
  saved: Record<string, unknown> = {},
  mappingType = PluginSettingTypeV3.Json,
  previewEntity?: PluginPreviewEntityV3,
  targets?: PluginMappingTargetV3[],
) {
  let currentSaved = saved;
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const requests: { name: string; variables: Record<string, unknown> }[] = [];
  let failPreview = false;
  const previewContext = {
    stash: { id: "42", title: "Current title" },
    catalog: { title: "Catalog title" },
    fields: ["title"],
  };
  const apollo = new ApolloClient({
    cache: new InMemoryCache(),
    link: new ApolloLink(
      (operation) =>
        new Observable((observer) => {
          requests.push({
            name: operation.operationName ?? "",
            variables: operation.variables,
          });
          if (
            operation.operationName === "PluginPreviewScenes" ||
            operation.operationName === "PluginPreviewImages"
          ) {
            const scene = operation.operationName === "PluginPreviewScenes";
            observer.next({
              data: {
                [scene ? "findScenes" : "findImages"]: {
                  [scene ? "scenes" : "images"]: [
                    {
                      id: "42",
                      title: "Preview item",
                      [scene ? "files" : "visual_files"]: [
                        { __typename: "VideoFile", path: "/media/example.mp4" },
                      ],
                    },
                  ],
                },
              },
            });
            observer.complete();
            return;
          }
          if (operation.operationName === "PluginSettingPreview") {
            if (failPreview)
              observer.error(new Error("Catalog is unavailable"));
            else {
              observer.next({
                data: { pluginSettingPreviewV3: previewContext },
              });
              observer.complete();
            }
            return;
          }
          if (operation.operationName === "PluginEvaluateMappings") {
            observer.next({
              data: { pluginEvaluateMappings: { title: "A quoted title" } },
            });
            observer.complete();
            return;
          }
          const input = operation.variables.input as Record<string, unknown>;
          const mappings =
            typeof input.mappings === "string"
              ? (JSON.parse(input.mappings) as Record<string, string>)
              : (input.mappings as Record<string, string> | undefined);
          if (mappings?.title === ".[") {
            observer.error(new Error("Invalid jq expression"));
          } else {
            observer.next({
              data: {
                updatePluginSettings: {
                  other: "external",
                  enabled: false,
                  ...currentSaved,
                  ...input,
                },
              },
            });
            observer.complete();
          }
        }),
    ),
  });
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  cleanup = async () => {
    await act(async () => root.unmount());
    apollo.stop();
    container.remove();
  };
  const settings: Parameters<typeof PluginSettingsForm>[0]["settings"] = [
    {
      __typename: "PluginSettingV3",
      name: "mappings",
      display_name: "Field mappings",
      description: "Map a field to a jq expression.",
      type: mappingType,
      editor: PluginSettingEditorV3.JqMap,
      default_value: mappingType === PluginSettingTypeV3.Json ? {} : "{}",
      options: [],
      mapping_targets:
        targets?.map((target) => ({
          ...target,
          __typename: "PluginMappingTargetV3" as const,
          description: target.description ?? null,
        })) ?? null,
      preview: previewEntity
        ? {
            __typename: "PluginSettingPreviewV3",
            entity: previewEntity,
            description: "Preview only",
          }
        : null,
    },
    {
      __typename: "PluginSettingV3",
      name: "other",
      mapping_targets: null,
      display_name: null,
      description: null,
      editor: null,
      type: PluginSettingTypeV3.String,
      default_value: "original",
      options: [],
      preview: null,
    },
    {
      __typename: "PluginSettingV3",
      name: "enabled",
      mapping_targets: null,
      display_name: null,
      description: null,
      editor: null,
      type: PluginSettingTypeV3.Boolean,
      default_value: false,
      options: [],
      preview: null,
    },
  ];
  const render = async (next: Record<string, unknown>) => {
    currentSaved = next;
    await act(async () =>
      root.render(
        <ApolloProvider client={apollo}>
          <IntlProvider locale="en">
            <PluginSettingsForm
              pluginId="fixture"
              settings={settings}
              saved={currentSaved}
            />
          </IntlProvider>
        </ApolloProvider>,
      ),
    );
  };
  const control = (label: string, index = 0) => {
    const labels = Array.from(container.querySelectorAll("label")).filter(
      (element) => element.textContent === label,
    );
    const element = document.getElementById(labels[index]?.htmlFor ?? "");
    if (
      !(
        element instanceof HTMLInputElement ||
        element instanceof HTMLTextAreaElement
      )
    ) {
      throw new Error(`Missing control: ${label} ${index}`);
    }
    return element;
  };
  const edit = async (label: string, value: string, index = 0) => {
    const element = control(label, index);
    await act(async () => {
      Object.getOwnPropertyDescriptor(
        element instanceof HTMLTextAreaElement
          ? HTMLTextAreaElement.prototype
          : HTMLInputElement.prototype,
        "value",
      )?.set?.call(element, value);
      element.dispatchEvent(new Event("input", { bubbles: true }));
    });
  };
  const click = async (name: string) => {
    const button = Array.from(container.querySelectorAll("button")).find(
      (element) =>
        (element.getAttribute("aria-label") ?? element.textContent) === name,
    );
    if (!button) throw new Error(`Missing button: ${name}`);
    await act(async () => button.click());
  };
  const submit = () =>
    act(async () => {
      container
        .querySelector("form")
        ?.dispatchEvent(
          new Event("submit", { bubbles: true, cancelable: true }),
        );
    });
  await render(saved);
  return {
    container,
    requests,
    render,
    control,
    edit,
    click,
    submit,
    previewContext,
    failPreview: () => {
      failPreview = true;
    },
  };
}

const expression =
  'select(.fields | index("title"))\n| .stash.title // "Untitled"';

it.each([PluginSettingTypeV3.String, PluginSettingTypeV3.Json])(
  "edits raw jq in %s mappings, retains failed drafts, and patches only edited settings",
  async (mappingType) => {
    const { container, requests, render, control, edit, click, submit } =
      await fixture({}, mappingType);
    expect(
      container.querySelector('[role="switch"]')?.getAttribute("aria-checked"),
    ).toBe("false");
    await click("Add mapping");
    await edit("Target field", "title");
    await edit("jq expression", ".[");
    await submit();
    expect(container.textContent).toContain("Invalid jq expression");
    expect(control("jq expression").value).toBe(".[");
    await render({ other: "external" });
    await edit("jq expression", expression);
    await submit();
    expect(requests.at(-1)?.variables).toEqual({
      plugin_id: "fixture",
      input: {
        mappings:
          mappingType === PluginSettingTypeV3.Json
            ? { title: expression }
            : JSON.stringify({ title: expression }),
      },
      reset: [],
    });
    expect(container.querySelector('[role="alert"]')).toBeNull();
    expect(control("jq expression").value).toBe(expression);
    expect(control("other").value).toBe("external");
  },
);

it.each([{ title: expression }, JSON.stringify({ title: expression })])(
  "opens saved mappings as unescaped expressions without rewriting untouched settings: %j",
  async (mappings) => {
    const { control, edit, submit, requests } = await fixture({ mappings });
    expect(control("Target field").value).toBe("title");
    expect(control("jq expression").value).toBe(expression);
    await edit("other", "changed");
    await submit();
    expect(requests.at(-1)?.variables.input).toEqual({ other: "changed" });
    expect(control("jq expression").value).toBe(expression);
  },
);

it("rejects incomplete and duplicate rows without losing them, and saves an empty map after removal", async () => {
  const { container, requests, control, click, edit, submit } = await fixture({
    mappings: { title: ".catalog.title" },
  });
  await click("Add mapping");
  await edit("Target field", "title", 1);
  await edit("jq expression", '"Replacement"', 1);
  await submit();
  expect(requests).toHaveLength(0);
  expect(container.textContent).toContain(
    "Each target field can only appear once.",
  );
  expect(control("Target field").getAttribute("aria-invalid")).toBe("true");
  const retainedExpression = control("jq expression", 1);
  await click("Remove mapping title");
  expect(control("jq expression")).toBe(retainedExpression);
  await edit("jq expression", "  ");
  await submit();
  expect(requests).toHaveLength(0);
  expect(container.textContent).toContain(
    "Each mapping needs a target field and a jq expression.",
  );
  await edit("jq expression", '"Replacement"');
  await submit();
  expect(requests.at(-1)?.variables.input).toEqual({
    mappings: { title: '"Replacement"' },
  });
  await click("Remove mapping title");
  await submit();
  expect(requests.at(-1)?.variables.input).toEqual({ mappings: {} });
});

it("previews the raw mapping draft with sample JSON without saving it", async () => {
  const { container, requests, click, edit, control } = await fixture({
    mappings: { title: ".catalog.title" },
  });
  await edit("jq expression", expression);
  await click("Preview mappings");
  await edit(
    "Sample input (JSON)",
    '{"fields":["title"],"stash":{"title":"A quoted title"}}',
  );
  await click("Test expression");
  expect(requests).toEqual([
    {
      name: "PluginEvaluateMappings",
      variables: {
        mappings: { title: expression },
        input: { fields: ["title"], stash: { title: "A quoted title" } },
      },
    },
  ]);
  expect(container.querySelector("pre")?.textContent).toContain(
    "A quoted title",
  );
  expect(control("jq expression").value).toBe(expression);
  const trigger = container.querySelector('[data-slot="collapsible-trigger"]');
  expect(trigger?.getAttribute("aria-expanded")).toBe("true");
  expect(
    document.getElementById(trigger?.getAttribute("aria-controls") ?? ""),
  ).not.toBeNull();
  await click("Preview mappings");
  expect(trigger?.getAttribute("aria-expanded")).toBe("false");
  await click("Preview mappings");
  expect(control("Sample input (JSON)").value).toContain("A quoted title");
  expect(requests).toHaveLength(1);
});

it("retains unsupported saved targets and prevents saving or testing them", async () => {
  const { container, requests, edit, click, submit, control } = await fixture(
    { mappings: { payload: ".catalog.title" } },
    PluginSettingTypeV3.Json,
    undefined,
    [
      {
        name: "title",
        label: "Title",
        type: "String",
        description: "The title",
      },
    ],
  );
  const target = container.querySelector('[data-slot="select-trigger"]');
  expect(target?.textContent).toContain("payload");
  expect(target?.getAttribute("aria-invalid")).toBe("true");
  await edit("jq expression", ".stash.title");
  await submit();
  await click("Preview mappings");
  await click("Test expression");
  expect(requests).toHaveLength(0);
  expect(container.textContent).toContain("Choose a supported target field");
  expect(control("jq expression").value).toBe(".stash.title");
  await click("Remove mapping payload");
  await submit();
  expect(requests.at(-1)?.variables.input).toEqual({ mappings: {} });
});

it("offers declared target labels and value formats and prevents duplicate selection", async () => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  const { container, requests, click, edit, submit } = await fixture(
    { mappings: { title: ".catalog.title" } },
    PluginSettingTypeV3.Json,
    undefined,
    [
      {
        name: "title",
        label: "Title",
        type: "String",
        description: "The title",
      },
      {
        name: "performer_ids",
        label: "Performers",
        type: "[ID!]",
        description: "Existing Stash performer IDs",
      },
    ],
  );
  expect(container.textContent).toContain("title: String");
  await click("Add mapping");
  const triggers = container.querySelectorAll<HTMLButtonElement>(
    '[data-slot="select-trigger"]',
  );
  const trigger = triggers[1];
  if (!trigger) throw new Error("Missing second target selector");
  await act(async () => trigger.click());
  const options = [
    ...document.querySelectorAll<HTMLElement>('[role="option"]'),
  ];
  expect(options.map((option) => option.textContent)).toEqual([
    "Title",
    "Performers",
  ]);
  const [title, performers] = options;
  if (!title || !performers) throw new Error("Missing target options");
  expect(title.getAttribute("aria-disabled")).toBe("true");
  await act(async () => performers.click());
  expect(trigger.textContent).toContain("Performers");
  expect(container.textContent).toContain("Existing Stash performer IDs");
  await edit("jq expression", '["12"]', 1);
  await submit();
  expect(requests.at(-1)?.variables.input).toEqual({
    mappings: { title: ".catalog.title", performer_ids: '["12"]' },
  });
  expect(
    [...container.querySelectorAll("button")].find(
      (button) => button.textContent === "Add mapping",
    )?.disabled,
  ).toBe(true);
});

it("preserves malformed saved data for repair, then switches to the mapping editor", async () => {
  const { control, edit, submit, requests } = await fixture({
    mappings: '{"title":42}',
  });
  expect(control("Saved mapping data").value).toBe('{"title":42}');
  await edit("Saved mapping data", '{"title":".catalog.title"}');
  expect(control("Target field").value).toBe("title");
  expect(control("jq expression").value).toBe(".catalog.title");
  await submit();
  expect(requests.at(-1)?.variables.input).toEqual({
    mappings: { title: ".catalog.title" },
  });
});

it("cancels row edits using the latest saved mappings", async () => {
  const { control, edit, click, render, requests } = await fixture({
    mappings: { title: ".catalog.title" },
  });
  await edit("jq expression", expression);
  await click("Add mapping");
  await render({ mappings: { details: ".catalog.details" } });
  await click("Cancel");
  expect(control("Target field").value).toBe("details");
  expect(control("jq expression").value).toBe(".catalog.details");
  expect(requests).toHaveLength(0);
});

it.each([PluginPreviewEntityV3.Scene, PluginPreviewEntityV3.Image])(
  "loads %s context, tests unsaved mappings, hides stale output and handles failed reloads without writes",
  async (entity) => {
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
    const {
      container,
      requests,
      control,
      edit,
      click,
      previewContext,
      failPreview,
    } = await fixture(
      { mappings: { title: ".catalog.title" } },
      PluginSettingTypeV3.Json,
      entity,
    );
    await click("Preview mappings");
    const label =
      entity === PluginPreviewEntityV3.Scene
        ? "Scene to preview"
        : "Image to preview";
    await act(async () => {
      const input = control(label);
      input.focus();
      input.dispatchEvent(
        new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }),
      );
    });
    const option = document.querySelector<HTMLElement>('[role="option"]');
    expect(option?.textContent).toContain("Preview item (#42)");
    await act(async () => option?.click());
    await click("Load entity data");
    expect(JSON.parse(control("Sample input (JSON)").value)).toEqual(
      previewContext,
    );
    const enter = new KeyboardEvent("keydown", {
      key: "Enter",
      bubbles: true,
      cancelable: true,
    });
    await act(async () => {
      control(label).dispatchEvent(enter);
    });
    expect(enter.defaultPrevented).toBe(true);
    expect(
      requests.find((request) => request.name === "PluginSettingPreview")
        ?.variables,
    ).toEqual({
      plugin_id: "fixture",
      setting: "mappings",
      entity_id: "42",
    });
    await edit("jq expression", ".stash.title");
    await click("Test expression");
    expect(requests.at(-1)).toEqual({
      name: "PluginEvaluateMappings",
      variables: {
        mappings: { title: ".stash.title" },
        input: previewContext,
      },
    });
    expect(container.querySelector("pre")).not.toBeNull();
    await edit("jq expression", ".catalog.title");
    expect(container.querySelector("pre")).toBeNull();
    await click("Test expression");
    await edit("Sample input (JSON)", "{ invalid");
    expect(container.querySelector("pre")).toBeNull();
    await click("Test expression");
    expect(container.querySelector('[role="alert"]')).not.toBeNull();
    failPreview();
    await click("Load entity data");
    expect(container.textContent).toContain("Catalog is unavailable");
    expect(control("Sample input (JSON)").value).toBe("{}");
    expect(
      requests.some(
        (request) =>
          request.name.startsWith("Update") ||
          request.name === "RunPluginOperation",
      ),
    ).toBe(false);
  },
);
