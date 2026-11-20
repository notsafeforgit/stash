import { bindHold, type HoldDetails } from "@ui-sdk/core";
import { type RefObject, useEffect, useRef } from "react";

/**
 * Runs `onHold` once a finger has pressed the element past ui-sdk's hold delay.
 * Mouse and pen presses are ignored. While a finger presses, the browser's own
 * long-press menu and iOS's callout stay closed. Keys are left to the element,
 * so Enter and Space keep their native activation.
 */
export function useTouchHold<T extends HTMLElement>(
  ref: RefObject<T | null>,
  {
    disabled = false,
    onHold,
  }: { disabled?: boolean; onHold: (details: HoldDetails) => void },
) {
  const latest = useRef({ disabled, onHold });
  useEffect(() => {
    latest.current = { disabled, onHold };
  });

  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    return bindHold(element, {
      keys: [],
      disabled: () => latest.current.disabled,
      onPress: ({ input }) => (input === "touch" ? undefined : false),
      onHold: (details) => latest.current.onHold(details),
    });
  }, [ref]);
}
