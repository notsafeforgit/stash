import {
  Slider as SliderPrimitive,
  SliderControl,
  SliderIndicator,
  SliderThumb,
  SliderTrack,
} from "@ui-sdk/react/slider";
import type * as React from "react";

import { cn } from "@/lib/utils";

type SliderPrimitiveProps = React.ComponentProps<typeof SliderPrimitive>;

type SliderProps = Omit<SliderPrimitiveProps, "className" | "children"> & {
  className?: string;
};

function Slider({
  className,
  defaultValue,
  value,
  min = 0,
  max = 100,
  "aria-label": ariaLabel,
  ...props
}: SliderProps) {
  const thumbCount = Array.isArray(value)
    ? value.length
    : Array.isArray(defaultValue)
      ? defaultValue.length
      : 1;
  const singleThumb = thumbCount === 1;

  return (
    <SliderPrimitive
      className={cn("data-[orientation=vertical]:h-full", className)}
      defaultValue={defaultValue}
      value={value}
      min={min}
      max={max}
      aria-label={singleThumb ? undefined : ariaLabel}
      {...props}
    >
      <SliderControl className="flex w-full items-center data-[orientation=vertical]:h-full data-[orientation=vertical]:min-h-40 data-[orientation=vertical]:w-auto data-[orientation=vertical]:flex-col">
        <SliderTrack className="relative grow">
          <SliderIndicator />
          {Array.from({ length: thumbCount }, (_, index) => (
            <SliderThumb
              key={index}
              index={index}
              aria-label={singleThumb ? ariaLabel : undefined}
            />
          ))}
        </SliderTrack>
      </SliderControl>
    </SliderPrimitive>
  );
}

export { Slider, type SliderProps };
