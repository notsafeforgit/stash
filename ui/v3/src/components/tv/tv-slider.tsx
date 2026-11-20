import { useState } from "react";
import { Slider } from "@/components/ui/slider";

export function TvSlider({
  value,
  min,
  max,
  step,
  label,
  onChange,
  onCommit,
  onCancel,
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
  onCancel?: () => void;
  onPreviewChange?: (visible: boolean) => void;
}) {
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
      onValueCommit={(next) =>
        commit(Array.isArray(next) ? (next[0] ?? value) : next)
      }
      onFocus={() => onPreviewChange?.(true)}
      onBlur={() => onPreviewChange?.(false)}
      onPointerDownCapture={() => {
        if (!disabled) onPreviewChange?.(true);
      }}
      onPointerCancel={() => {
        setDraft(null);
        onCancel?.();
        onPreviewChange?.(false);
      }}
    />
  );
}
