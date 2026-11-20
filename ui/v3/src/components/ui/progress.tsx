import {
  Progress as ProgressPrimitive,
  ProgressIndicator as ProgressIndicatorPrimitive,
  ProgressLabel as ProgressLabelPrimitive,
  ProgressTrack as ProgressTrackPrimitive,
  ProgressValue as ProgressValuePrimitive,
} from "@ui-sdk/react/progress";
import type * as React from "react";

import { cn } from "@/lib/utils";

type ProgressProps = Omit<
  React.ComponentProps<typeof ProgressPrimitive>,
  "className" | "children"
> & {
  className?: string;
  children?: React.ReactNode;
};

function Progress({ className, children, value, ...props }: ProgressProps) {
  return (
    <ProgressPrimitive value={value} className={className} {...props}>
      {/*
        When children are supplied, the caller is composing manually
        (Track / Indicator / Label / Value of their own choosing) — do
        not auto-render the default Track + Indicator pair, which would
        result in two stacked tracks. Caller-less use stays at one-line:
        `<Progress value={pct} />` renders the default bar.
      */}
      {children ?? (
        <ProgressTrack>
          <ProgressIndicator />
        </ProgressTrack>
      )}
    </ProgressPrimitive>
  );
}

type ProgressTrackProps = Omit<
  React.ComponentProps<typeof ProgressTrackPrimitive>,
  "className"
> & {
  className?: string;
};

function ProgressTrack({ className, ...props }: ProgressTrackProps) {
  return <ProgressTrackPrimitive className={className} {...props} />;
}

type ProgressIndicatorProps = Omit<
  React.ComponentProps<typeof ProgressIndicatorPrimitive>,
  "className"
> & {
  className?: string;
};

function ProgressIndicator({ className, ...props }: ProgressIndicatorProps) {
  return (
    <ProgressIndicatorPrimitive
      // `transition-[width]` not `transition-all`: the indicator's inline
      // width changes on every value tick, and transitioning every property
      // visibly stalls at the end of high-frequency updates (download progress
      // fires several times per second). Pinning the transition to width keeps
      // the bar smooth.
      className={cn("transition-[width] duration-150 ease-linear", className)}
      {...props}
    />
  );
}

type ProgressLabelProps = Omit<
  React.ComponentProps<typeof ProgressLabelPrimitive>,
  "className"
> & {
  className?: string;
};

function ProgressLabel({ className, ...props }: ProgressLabelProps) {
  return <ProgressLabelPrimitive className={className} {...props} />;
}

type ProgressValueProps = Omit<
  React.ComponentProps<typeof ProgressValuePrimitive>,
  "className"
> & {
  className?: string;
};

function ProgressValue({ className, ...props }: ProgressValueProps) {
  return <ProgressValuePrimitive className={className} {...props} />;
}

export {
  Progress,
  ProgressTrack,
  ProgressIndicator,
  ProgressLabel,
  ProgressValue,
};
