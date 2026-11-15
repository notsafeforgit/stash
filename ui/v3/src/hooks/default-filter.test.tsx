// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, expect, it, vi } from "vitest";
import type { View } from "src/components/list/views";
import {
  DefaultFilterAction,
  FilterMode,
  type SavedFilterDataFragment,
} from "src/core/generated-graphql";
import type { ListFilterModel } from "src/models/list-filter/filter";
import { useDefaultFilterActions } from "./default-filter";

const mocks = vi.hoisted(() => ({
  configureDefaultFilter: vi.fn(async () => ({})),
  conflict: undefined as
    | { revision: number; import_error?: string }
    | undefined,
}));

vi.mock("./config", () => ({
  useConfigurationContextOptional: () =>
    mocks.conflict
      ? {
          configuration: {
            ui: { defaultFilterConflicts: { scenes: mocks.conflict } },
          },
        }
      : undefined,
  useConfigureDefaultFilter: () => [
    mocks.configureDefaultFilter,
    { loading: false },
  ],
}));

vi.mock("./toast", () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}));

// Criteria the list model does not understand must reach the default unchanged.
const storedCriteria = {
  root: {
    group: {
      operator: "AND",
      children: [
        {
          condition: {
            field: "future_criterion",
            value: {
              value: "keep",
              modifier: "EQUALS",
              extra: { nested: [1] },
            },
          },
        },
      ],
    },
  },
};

const saved = {
  __typename: "SavedFilter",
  id: "7",
  mode: FilterMode.Scenes,
  name: "Favourites",
  find_filter: {
    __typename: "SavedFindFilterType",
    q: "beach",
    page: 1,
    per_page: 40,
    sort: "date",
    direction: "DESC",
  },
  filter_ast: storedCriteria,
  ui_options: {},
} as unknown as SavedFilterDataFragment;

type Actions = ReturnType<typeof useDefaultFilterActions>;
let latest: Actions | undefined;

function Harness() {
  // The list model is a stub with no methods: setSaved must not touch it.
  latest = useDefaultFilterActions("scenes" as View, {} as ListFilterModel);
  return null;
}

let cleanup: (() => Promise<void>) | undefined;

afterEach(async () => {
  await cleanup?.();
  cleanup = undefined;
  latest = undefined;
  mocks.conflict = undefined;
  mocks.configureDefaultFilter.mockClear();
  document.body.innerHTML = "";
});

async function mount() {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  await act(async () => {
    root.render(
      <IntlProvider locale="en-GB" messages={{}}>
        <Harness />
      </IntlProvider>,
    );
  });
  cleanup = async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
  };
  if (!latest) throw new Error("Harness did not render");
  return latest;
}

it("sets a saved filter's stored criteria as the view default", async () => {
  const actions = await mount();

  await act(async () => {
    await actions.setSaved(saved);
  });

  expect(mocks.configureDefaultFilter).toHaveBeenCalledTimes(1);
  expect(mocks.configureDefaultFilter).toHaveBeenCalledWith({
    variables: {
      input: {
        view: "scenes",
        action: DefaultFilterAction.Set,
        filter: {
          mode: FilterMode.Scenes,
          find_filter: {
            q: "beach",
            page: 1,
            per_page: 40,
            sort: "date",
            direction: "DESC",
          },
          filter_ast: storedCriteria,
          ui_options: {},
        },
        expected_revision: undefined,
      },
    },
  });
});

it("sends the reviewed revision when a migrated conflict is pending", async () => {
  mocks.conflict = { revision: 4 };
  const actions = await mount();

  await act(async () => {
    await actions.setSaved(saved);
  });

  expect(mocks.configureDefaultFilter).toHaveBeenCalledWith({
    variables: expect.objectContaining({
      input: expect.objectContaining({
        action: DefaultFilterAction.Set,
        expected_revision: 4,
      }),
    }),
  });
});
