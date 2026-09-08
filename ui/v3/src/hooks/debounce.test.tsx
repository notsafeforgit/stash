// @vitest-environment jsdom
import { act, StrictMode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, expectTypeOf, it, vi } from "vitest";
import {
  useDebounce,
  useDebouncedState,
  type DebounceSettings,
} from "./debounce";

let root: Root;
let container: HTMLDivElement;
beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it("honors leading, trailing and maxWait with an honest return type", async () => {
  const callback = vi.fn((value: number) => value * 2);
  let run: ReturnType<typeof useDebounce<[number], number>> | undefined;
  function Probe({ options }: { options: DebounceSettings }) {
    run = useDebounce(callback, 100, options);
    expectTypeOf<ReturnType<typeof run>>().toEqualTypeOf<number | undefined>();
    return null;
  }
  await act(async () =>
    root.render(
      <StrictMode>
        <Probe options={{ leading: true, trailing: false }} />
      </StrictMode>,
    ),
  );
  callback.mockClear();
  run?.cancel();
  expect(run?.(3)).toBe(6);
  run?.(4);
  await act(async () => vi.advanceTimersByTime(100));
  expect(callback.mock.calls).toEqual([[3]]);

  await act(async () => root.render(<Probe options={{ maxWait: 150 }} />));
  callback.mockClear();
  run?.cancel();
  run?.(1);
  await act(async () => vi.advanceTimersByTime(80));
  run?.(2);
  await act(async () => vi.advanceTimersByTime(70));
  expect(callback.mock.calls).toEqual([[2]]);
});

it("cancels replaced and unmounted work, and explicitly flushes persisted edits", async () => {
  const callback = vi.fn();
  let run: ReturnType<typeof useDebounce<[], void>> | undefined;
  function Probe({ wait, flush = false }: { wait: number; flush?: boolean }) {
    run = useDebounce(callback, wait, { flushOnUnmount: flush });
    return null;
  }
  await act(async () => root.render(<Probe wait={50} />));
  run?.();
  await act(async () => root.render(<Probe wait={100} />));
  await act(async () => vi.advanceTimersByTime(60));
  expect(callback).not.toHaveBeenCalled();
  run?.();
  await act(async () => root.render(null));
  await act(async () => vi.advanceTimersByTime(100));
  expect(callback).not.toHaveBeenCalled();
  await act(async () => root.render(<Probe wait={100} flush />));
  run?.();
  await act(async () => root.render(null));
  expect(callback).toHaveBeenCalledOnce();
});

it("an instant update cancels an obsolete delayed value", async () => {
  const write = vi.fn();
  let state: ReturnType<typeof useDebouncedState<number>> | undefined;
  function Probe() {
    state = useDebouncedState<number>(0, write, 50);
    return null;
  }
  await act(async () => root.render(<Probe />));
  await act(async () => {
    state?.[1](1);
    state?.[2](2);
  });
  await act(async () => vi.advanceTimersByTime(100));
  expect(write.mock.calls).toEqual([[2]]);
});
