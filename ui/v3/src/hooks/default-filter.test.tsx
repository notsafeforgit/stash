// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, expect, it, vi } from "vitest";
import type { View } from "src/components/list/views";
import { DefaultFilterAction } from "src/core/generated-graphql";
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

type Actions = ReturnType<typeof useDefaultFilterActions>;
let latest: Actions | undefined;

function Harness() {
  // Clearing doesn't read the list model.
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

it("sends the reviewed revision when a migrated conflict is pending", async () => {
  mocks.conflict = { revision: 4 };
  const actions = await mount();

  await act(async () => {
    await actions.clear();
  });

  expect(mocks.configureDefaultFilter).toHaveBeenCalledWith({
    variables: expect.objectContaining({
      input: expect.objectContaining({
        action: DefaultFilterAction.Clear,
        expected_revision: 4,
      }),
    }),
  });
});
