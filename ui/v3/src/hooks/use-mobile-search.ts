import { useEffect, useRef, useState } from "react";
import { flushSync } from "react-dom";

// Leave room for the input's focus ring without changing its layout bounds.
const EXPANDED_CLIP = "inset(-4px -4px -4px -4px)";

/** Search replaces a toolbar row, revealing from its trigger without moving or
 * scaling the focused input. Focus stays synchronous with the opening gesture. */
export function useMobileSearch() {
  const [isOpen, setIsOpen] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const rowRef = useRef<HTMLDivElement>(null);
  const collapsedClipRef = useRef(EXPANDED_CLIP);
  const animationRef = useRef<Animation | null>(null);

  useEffect(() => () => animationRef.current?.cancel(), []);

  function animate(keyframes: Keyframe[], onFinish?: () => void) {
    animationRef.current?.cancel();
    animationRef.current = null;
    const row = rowRef.current;
    if (!row || window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      onFinish?.();
      return;
    }

    const animation = row.animate(keyframes, {
      duration: 180,
      easing: "cubic-bezier(0.2, 0, 0, 1)",
      fill: "forwards",
    });
    animationRef.current = animation;
    animation.onfinish = () => {
      animationRef.current = null;
      onFinish?.();
      animation.cancel();
    };
  }

  function openSearch() {
    const trigger = buttonRef.current?.getBoundingClientRect();
    flushSync(() => setIsOpen(true));
    inputRef.current?.focus();
    const row = rowRef.current?.getBoundingClientRect();
    if (trigger && row && row.width > 0) {
      // Percentages keep the return destination proportional after rotation.
      const left = Math.max(0, ((trigger.left - row.left) / row.width) * 100);
      const right = Math.max(
        0,
        ((row.right - trigger.right) / row.width) * 100,
      );
      collapsedClipRef.current = `inset(-4px ${right}% -4px ${left}% round 8px)`;
    }
    animate([
      { clipPath: collapsedClipRef.current, opacity: 0 },
      { clipPath: EXPANDED_CLIP, opacity: 1 },
    ]);
  }

  function closeSearch() {
    // Blur flushes the list input's debounce before it leaves the toolbar.
    inputRef.current?.blur();
    const style = rowRef.current && getComputedStyle(rowRef.current);
    animate(
      [
        {
          clipPath:
            style?.clipPath === "none" ? EXPANDED_CLIP : style?.clipPath,
          opacity: style?.opacity ?? 1,
        },
        { clipPath: collapsedClipRef.current, opacity: 0 },
      ],
      () => {
        flushSync(() => setIsOpen(false));
        buttonRef.current?.focus({ preventScroll: true });
      },
    );
  }

  return { isOpen, inputRef, buttonRef, rowRef, openSearch, closeSearch };
}
