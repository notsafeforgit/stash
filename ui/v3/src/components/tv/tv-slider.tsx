import {
  createContext,
  useContext,
  useRef,
  useState,
  type PointerEvent,
} from "react";
import { Slider } from "@/components/ui/slider";
import type { TvRotation } from "@/core/tv/settings";

const RotationContext = createContext<TvRotation>("normal");
export const TvRotationProvider = RotationContext.Provider;

/** Base UI owns semantics, keyboard input and ordinary dragging. For a rotated
 * presentation only, map pointer input to the visible track's axis locally. */
export function TvSlider({
  value,
  min,
  max,
  step,
  label,
  onChange,
  onCommit,
  onPreviewChange,
  disabled,
}: {
  value: number;
  min: number;
  max: number;
  step: number;
  label: string;
  disabled?: boolean;
  onChange?: (value: number) => void;
  onCommit?: (value: number) => void;
  onPreviewChange?: (visible: boolean) => void;
}) {
  const rotation = useContext(RotationContext);
  const pointer = useRef<number | null>(null);
  const [draft, setDraft] = useState<number | null>(null);
  const change = (next: number) => {
    setDraft(next);
    onChange?.(next);
  };
  const commit = (next: number) => {
    onCommit?.(next);
    setDraft(null);
    onPreviewChange?.(false);
  };
  const position = (event: PointerEvent<HTMLElement>) => {
    const track = event.currentTarget.querySelector(
      '[data-slot="slider-track"]',
    );
    const rect = (track ?? event.currentTarget).getBoundingClientRect();
    const progress =
      (rotation === "clockwise"
        ? event.clientY - rect.top
        : rect.bottom - event.clientY) / Math.max(1, rect.height);
    return Math.max(
      min,
      Math.min(max, min + Math.round((progress * (max - min)) / step) * step),
    );
  };
  return (
    <Slider
      aria-label={label}
      value={[draft ?? value]}
      min={min}
      max={max}
      step={step}
      disabled={disabled}
      className="py-3"
      onValueChange={(next) =>
        change(Array.isArray(next) ? (next[0] ?? value) : next)
      }
      onValueCommitted={(next) =>
        commit(Array.isArray(next) ? (next[0] ?? value) : next)
      }
      onFocus={() => onPreviewChange?.(true)}
      onBlur={() => onPreviewChange?.(false)}
      onPointerDownCapture={(event) => {
        if (disabled) return;
        onPreviewChange?.(true);
        if (rotation === "normal" || event.button !== 0) return;
        event.preventDefault();
        event.stopPropagation();
        pointer.current = event.pointerId;
        event.currentTarget.setPointerCapture(event.pointerId);
        event.currentTarget
          .querySelector<HTMLElement>('[role="slider"]')
          ?.focus();
        change(position(event));
      }}
      onPointerMoveCapture={(event) => {
        if (pointer.current !== event.pointerId) return;
        event.preventDefault();
        event.stopPropagation();
        change(position(event));
      }}
      onPointerUpCapture={(event) => {
        if (pointer.current !== event.pointerId) return;
        pointer.current = null;
        event.preventDefault();
        event.stopPropagation();
        commit(position(event));
      }}
      onPointerCancel={() => {
        pointer.current = null;
        setDraft(null);
        onPreviewChange?.(false);
      }}
    />
  );
}
