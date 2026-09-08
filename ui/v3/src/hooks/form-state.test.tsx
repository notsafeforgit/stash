// @vitest-environment jsdom
import {
  act,
  startTransition,
  StrictMode,
  Suspense,
  useLayoutEffect,
  useState,
} from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useEditableRows } from "./use-editable-rows";
import { useLabelCache } from "./use-label-cache";
import { useCommittedRef } from "./use-committed-ref";
let root: Root, container: HTMLDivElement;
beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

it("preserves a focused row's DOM identity when an earlier row is removed", async () => {
  let remove: () => void = () => {};
  let reset: () => void = () => {};
  function Fields() {
    const [values, setValues] = useState(["first", "second", "third"]);
    const rows = useEditableRows(values, setValues);
    useLayoutEffect(() => {
      remove = () => rows.remove(0);
      reset = () => setValues(["reset", "third", "new"]);
    });
    return rows.rows.map((row) => (
      <input key={row.key} value={row.value} readOnly aria-label={row.value} />
    ));
  }
  await act(async () =>
    root.render(
      <StrictMode>
        <Fields />
      </StrictMode>,
    ),
  );
  const third = container.querySelectorAll("input")[2];
  third?.focus();
  await act(async () => remove());
  expect(container.querySelectorAll("input")[1]).toBe(third);
  expect(document.activeElement).toBe(third);
  await act(async () => reset());
  expect(container.querySelectorAll("input")[1]).toBe(third);
  expect(
    Array.from(container.querySelectorAll("input"), (input) => input.value),
  ).toEqual(["reset", "third", "new"]);
});

it("an abandoned label update cannot change a committed callback", async () => {
  let read = () => "";
  const suspended = new Promise<void>(() => {});
  function Labels({
    label,
    suspend = false,
  }: {
    label: string;
    suspend?: boolean;
  }) {
    const [known] = useLabelCache([["1", label]]);
    const committed = useCommittedRef(known);
    useLayoutEffect(() => {
      read = () => committed.current.get("1") ?? "";
    }, []);
    if (suspend) throw suspended;
    return <output>{known.get("1")}</output>;
  }
  function render(label: string, suspend = false) {
    root.render(
      <StrictMode>
        <Suspense fallback="Loading">
          <Labels label={label} suspend={suspend} />
        </Suspense>
      </StrictMode>,
    );
  }
  await act(async () => render("committed"));
  await act(async () => startTransition(() => render("abandoned", true)));
  expect(read()).toBe("committed");
  expect(container.textContent).toBe("committed");
  await act(async () => render("edited"));
  expect(read()).toBe("edited");
});
