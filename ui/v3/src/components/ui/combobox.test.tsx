// @vitest-environment jsdom
import { act, type ReactElement, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  Combobox,
  ComboboxChip,
  ComboboxContent,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "./combobox";

const recorded = vi.hoisted(() => new Map<string, Record<string, unknown>[]>());

// The ui-sdk parts are stubbed to record the props the wrapper passes them.
vi.mock("@ui-sdk/react/combobox", async () => {
  const { createElement } =
    await vi.importActual<typeof import("react")>("react");
  function part(name: string) {
    return function Part(props: Record<string, unknown>) {
      recorded.set(name, [...(recorded.get(name) ?? []), props]);
      const { children } = props;
      return createElement(
        "div",
        { "data-part": name },
        typeof children === "function" ? null : (children as ReactNode),
      );
    };
  }
  return {
    Combobox: part("Combobox"),
    ComboboxChip: part("ComboboxChip"),
    ComboboxChipRemove: part("ComboboxChipRemove"),
    ComboboxChips: part("ComboboxChips"),
    ComboboxClear: part("ComboboxClear"),
    ComboboxContent: part("ComboboxContent"),
    ComboboxEmpty: part("ComboboxEmpty"),
    ComboboxGroup: part("ComboboxGroup"),
    ComboboxGroupLabel: part("ComboboxGroupLabel"),
    ComboboxInput: part("ComboboxInput"),
    ComboboxItem: part("ComboboxItem"),
    ComboboxItemIndicator: part("ComboboxItemIndicator"),
    ComboboxLabel: part("ComboboxLabel"),
    ComboboxList: part("ComboboxList"),
    ComboboxSeparator: part("ComboboxSeparator"),
    ComboboxTrigger: part("ComboboxTrigger"),
    ComboboxValue: part("ComboboxValue"),
  };
});

type Person = { id: string; name: string };

const ada: Person = { id: "1", name: "Ada" };
const grace: Person = { id: "2", name: "Grace" };
const people: Person[] = [ada, grace];

const messages = {
  "actions.clear": "Clear",
  "actions.remove": "Remove",
  "actions.show_options": "Show options",
};

let root: Root | undefined;
let container: HTMLElement | undefined;

beforeEach(() => {
  recorded.clear();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
});

afterEach(async () => {
  if (root) await act(async () => root?.unmount());
  container?.remove();
  root = undefined;
  container = undefined;
  vi.unstubAllGlobals();
});

async function render(node: ReactElement) {
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  await act(async () =>
    root?.render(
      <IntlProvider locale="en" messages={messages}>
        {node}
      </IntlProvider>,
    ),
  );
}

function lastProps(name: string): Record<string, unknown> {
  const calls = recorded.get(name);
  const props = calls?.at(-1);
  if (!props) throw new Error(`${name} was not rendered`);
  return props;
}

it("maps Base UI-named props onto the ui-sdk root", async () => {
  await render(
    <Combobox<Person>
      items={people}
      filter={null}
      inputValue="ad"
      value={ada}
      itemToStringLabel={(person) => person.name}
      itemToStringValue={(person) => person.id}
    >
      <ComboboxInput placeholder="Search" />
    </Combobox>,
  );
  const props = lastProps("Combobox");
  expect(props.items).toEqual([
    { value: ada, label: "Ada" },
    { value: grace, label: "Grace" },
  ]);
  expect(props.filter).toBe(false);
  expect(props.query).toBe("ad");
  expect((props.itemToValue as (item: Person) => string)(grace)).toBe("2");
  expect((props.itemToLabel as (item: Person) => string)(grace)).toBe("Grace");
});

it("reports a clear as input-clear and keeps typing as input-change", async () => {
  const onInputValueChange = vi.fn();
  await render(
    <Combobox<Person> items={people} onInputValueChange={onInputValueChange}>
      <ComboboxInput />
    </Combobox>,
  );
  const onQueryChange = lastProps("Combobox").onQueryChange as (
    query: string,
    details: Record<string, unknown>,
  ) => void;
  onQueryChange("", { reason: "clear-press", source: "mouse" });
  onQueryChange("g", { reason: "input-change", source: "keyboard" });
  expect(
    onInputValueChange.mock.calls.map(([, details]) => details.reason),
  ).toEqual(["input-clear", "input-change"]);
});

it("renders each visible item from a list render function", async () => {
  await render(
    <Combobox<Person> items={people} filteredItems={[grace]}>
      <ComboboxInput />
      <ComboboxContent>
        <ComboboxList>
          {(person: Person) => (
            <ComboboxItem key={person.id} value={person}>
              <span>{person.name}</span>
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>,
  );
  const renderItems = lastProps("ComboboxList").children as (state: {
    items: unknown[];
  }) => ReactNode[];
  const items = lastProps("Combobox").items as { value: unknown }[];
  const rendered = renderItems({ items }) as ReactElement<{ value: Person }>[];
  expect(rendered.map((element) => element.props.value)).toEqual([grace]);
});

it("labels a JSX item from the root's item label", async () => {
  await render(
    <Combobox<Person>
      items={people}
      itemToStringLabel={(person) => person.name}
    >
      <ComboboxInput />
      <ComboboxItem value={ada}>
        <span>Ada Lovelace</span>
      </ComboboxItem>
    </Combobox>,
  );
  expect(lastProps("ComboboxItem").label).toBe("Ada");
});

it("names the trigger and clear controls", async () => {
  await render(
    <Combobox<string> defaultValue="a">
      <ComboboxInput showClear />
      <ComboboxChip value="a">Alpha</ComboboxChip>
    </Combobox>,
  );
  expect(lastProps("ComboboxTrigger")["aria-label"]).toBe("Show options");
  expect(lastProps("ComboboxClear")["aria-label"]).toBe("Clear");
  expect(lastProps("ComboboxChipRemove")["aria-label"]).toBe("Remove");
});
