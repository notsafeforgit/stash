// @vitest-environment jsdom
import { act, StrictMode, useState, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import {
  ApolloClient,
  ApolloLink,
  InMemoryCache,
  Observable,
} from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
import { ShortcutProvider } from "@/components/shortcut-provider";
import { useForm, useStore } from "@tanstack/react-form";
import { FilterMode } from "@/core/generated-graphql";
import { BulkCustomFieldsField } from "./bulk-custom-fields-field";
import {
  bulkCustomFieldsInput,
  hasBulkCustomFieldChanges,
  type BulkCustomFieldsValue,
} from "./bulk-custom-fields";
import { useBulkCustomFields } from "./use-bulk-custom-fields";
import type { CustomFieldNameInputProps } from "./custom-field-name-input";

// The browser checks cover the suggestion popup and its keyboard behavior.
// Here a plain name input keeps the test focused on form state and submission.
vi.mock("@/components/forms/custom-field-name-input", () => ({
  CustomFieldNameInput: ({
    options: _options,
    loading: _loading,
    onChange,
    ...props
  }: CustomFieldNameInputProps) => (
    <input
      {...props}
      role="combobox"
      aria-expanded="false"
      onChange={(event) => onChange(event.target.value)}
    />
  ),
}));

let root: Root;
let container: HTMLDivElement;
let client: ApolloClient;
const submissions: { operation: string; input: unknown }[] = [];
const nameQueries: unknown[] = [];
const summaryQueries: unknown[] = [];
let failSummary = false;
let delaySummary = false;
const pendingSummaries: (() => void)[] = [];

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  submissions.length = 0;
  nameQueries.length = 0;
  summaryQueries.length = 0;
  pendingSummaries.length = 0;
  failSummary = false;
  delaySummary = false;
  client = new ApolloClient({
    cache: new InMemoryCache(),
    link: new ApolloLink(
      (operation) =>
        new Observable((observer) => {
          if (operation.operationName === "BulkCustomFieldSummary") {
            summaryQueries.push(operation.variables.input);
            const respond = () => {
              if (failSummary) observer.error(new Error("Summary unavailable"));
              else
                observer.next({
                  data: {
                    bulkCustomFieldSummary: operation.variables.input.ids
                      ? {
                          count: 2,
                          shared_names: ["blank", "keep", "obsolete", "score"],
                          partial_names: ["private"],
                        }
                      : {
                          count: 12,
                          shared_names: ["keep", "score"],
                          partial_names: ["blank", "obsolete", "private"],
                        },
                  },
                });
              observer.complete();
            };
            if (delaySummary) pendingSummaries.push(respond);
            else respond();
            return;
          }
          if (operation.operationName === "CustomFieldNames") {
            nameQueries.push(operation.variables.mode);
            observer.next({
              data: { customFieldNames: ["score", "obsolete", "source path"] },
            });
          } else {
            observer.error(
              new Error(`Unexpected operation: ${operation.operationName}`),
            );
          }
          observer.complete();
        }),
    ),
  });
});

afterEach(async () => {
  await act(async () => root.unmount());
  client.stop();
  container.remove();
  vi.unstubAllGlobals();
});

async function render(content: ReactNode) {
  await act(async () =>
    root.render(
      <StrictMode>
        <ApolloProvider client={client}>
          <IntlProvider locale="en-GB" messages={flattenMessages(messages)}>
            <ShortcutProvider>{content}</ShortcutProvider>
          </IntlProvider>
        </ApolloProvider>
      </StrictMode>,
    ),
  );
}

function button(scope: ParentNode, label: string) {
  const result = Array.from(scope.querySelectorAll("button")).find(
    (item) =>
      item.getAttribute("aria-label") === label ||
      item.textContent?.trim() === label,
  );
  if (!result) throw new Error(`Button not found: ${label}`);
  return result;
}

function customFields() {
  const result = Array.from(document.querySelectorAll("fieldset")).find(
    (item) => item.querySelector("legend")?.textContent === "Custom Fields",
  );
  if (!result) throw new Error("Custom fields section not found");
  return result;
}

async function click(scope: ParentNode, label: string) {
  await act(async () => button(scope, label).click());
}

async function type(input: HTMLInputElement | undefined | null, value: string) {
  if (!input) throw new Error("Input not found");
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype,
      "value",
    )?.set?.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

function sharedField(name: string) {
  const result = Array.from(customFields().querySelectorAll("fieldset")).find(
    (item) => item.querySelector("legend")?.textContent === name,
  );
  if (!result) throw new Error("Shared field not found: " + name);
  return result;
}

const items = [{ id: "1" }, { id: "2" }];
const matching = { findFilter: { q: "target" } };

function Editor() {
  const [all, setAll] = useState(false);
  const state = useBulkCustomFields({
    open: true,
    mode: FilterMode.Tags,
    items,
    matching: all ? matching : undefined,
  });
  const form = useForm({
    defaultValues: { custom_fields: undefined as BulkCustomFieldsValue },
    onSubmit: ({ value }) => {
      submissions.push({
        operation: "save",
        input: bulkCustomFieldsInput(value.custom_fields),
      });
    },
  });
  const canSubmit = useStore(form.store, (state) => state.canSubmit);
  const editing = useStore(form.store, (state) =>
    hasBulkCustomFieldChanges(state.values.custom_fields),
  );
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void form.handleSubmit();
      }}
    >
      <form.Field name="custom_fields" validators={{ onChange: state.schema }}>
        {(field) => (
          <BulkCustomFieldsField
            value={field.state.value}
            onChange={field.handleChange}
            entityMode={FilterMode.Tags}
            state={state}
          />
        )}
      </form.Field>
      <button
        type="submit"
        disabled={!canSubmit || (editing && !state.summary)}
      >
        Save
      </button>
      <button type="button" onClick={() => form.reset()}>
        Reset
      </button>
      <button
        type="button"
        onClick={() => {
          form.setFieldValue("custom_fields", undefined);
          setAll((current) => !current);
        }}
      >
        All matching
      </button>
    </form>
  );
}

async function editFields() {
  await click(sharedField("score"), "Set");
  await type(sharedField("score").querySelector("input"), "12.5");
  await click(sharedField("blank"), "Clear value");
  await click(sharedField("obsolete"), "Remove field");
}

it("offers only shared fields and combines keep, set, clear, remove, and additions", async () => {
  await render(<Editor />);
  expect(summaryQueries).toEqual([{ mode: "TAGS", ids: ["1", "2"] }]);
  expect(() => sharedField("private")).toThrow();
  expect(sharedField("keep").textContent).toContain("even when values differ");
  await editFields();
  await click(customFields(), "Add new field");
  await type(
    customFields().querySelector<HTMLInputElement>('input[role="combobox"]'),
    "new field",
  );
  const values = customFields().querySelectorAll<HTMLInputElement>(
    'input[data-slot="input"]',
  );
  await type(values[1], "0012");
  expect(nameQueries).toContain("TAGS");
  await click(document, "Save");
  expect(submissions[0]?.input).toEqual({
    partial: { score: 12.5, blank: "", "new field": "0012" },
    remove: ["obsolete"],
  });
});

it("blocks additions that collide with shared or partially shared fields", async () => {
  await render(<Editor />);
  await click(customFields(), "Add new field");
  const name = customFields().querySelector<HTMLInputElement>(
    'input[role="combobox"]',
  );
  for (const value of ["", "score", "private", "é".repeat(33)]) {
    await type(name, value);
    expect(button(document, "Save").disabled).toBe(true);
  }
  await type(name, "fresh");
  expect(button(document, "Save").disabled).toBe(false);
  await click(customFields(), "Add new field");
  await type(
    customFields().querySelectorAll<HTMLInputElement>(
      'input[role="combobox"]',
    )[1],
    "fresh",
  );
  expect(button(document, "Save").disabled).toBe(true);
  expect(
    customFields().querySelectorAll('input[aria-invalid="true"]'),
  ).toHaveLength(2);
});

it("saves the latest focused value and keeps distinct existing values when set returns to keep", async () => {
  await render(<Editor />);
  await editFields();
  await click(sharedField("score"), "Keep");
  await click(sharedField("score"), "Set");
  const input = sharedField("score").querySelector("input");
  expect(input?.value).toBe("12.5");
  await act(async () => input?.focus());
  await type(input, "27.5");
  await act(async () => customFields().closest("form")?.requestSubmit());
  expect(submissions[0]?.input).toEqual({
    partial: { score: 27.5, blank: "" },
    remove: ["obsolete"],
  });
  await click(document, "Reset");
  await click(document, "Save");
  expect(submissions[1]?.input).toBeUndefined();
});

it("discards additions without removing existing fields and preserves remaining row identity", async () => {
  await render(<Editor />);
  await click(customFields(), "Add new field");
  await type(
    customFields().querySelector<HTMLInputElement>('input[role="combobox"]'),
    "first",
  );
  await click(customFields(), "Add new field");
  const next = customFields().querySelectorAll<HTMLInputElement>(
    'input[role="combobox"]',
  )[1];
  await type(next, "second");
  await click(customFields(), "Discard new field");
  expect(customFields().querySelector('input[role="combobox"]')).toBe(next);
  await click(customFields(), "Discard new field");
  await click(document, "Save");
  expect(submissions[0]?.input).toBeUndefined();
});

it("resets edits and waits for the new complete scope instead of reusing selected-item names", async () => {
  await render(<Editor />);
  await editFields();
  delaySummary = true;
  await click(document, "All matching");
  expect(customFields().querySelectorAll("fieldset")).toHaveLength(0);
  expect(customFields().querySelector('input[role="combobox"]')).toBeNull();
  expect(summaryQueries[1]).toEqual({
    mode: "TAGS",
    find_filter: { q: "target" },
  });
  await act(async () => {
    for (const respond of pendingSummaries.splice(0)) respond();
  });
  expect(() => sharedField("obsolete")).toThrow();
  expect(() => sharedField("blank")).toThrow();
  await click(document, "Save");
  expect(submissions[0]?.input).toBeUndefined();
  await click(sharedField("score"), "Clear value");
  await click(document, "Save");
  expect(submissions[1]?.input).toEqual({ partial: { score: "" }, remove: [] });
});

it("allows unrelated edits after summary failure and provides a working retry", async () => {
  failSummary = true;
  await render(<Editor />);
  expect(customFields().textContent).toContain(
    "Could not check shared custom fields",
  );
  expect(customFields().querySelectorAll("fieldset")).toHaveLength(0);
  expect(button(document, "Save").disabled).toBe(false);
  failSummary = false;
  await click(customFields(), "Retry");
  expect(sharedField("score")).toBeTruthy();
  expect(summaryQueries).toHaveLength(2);
});
