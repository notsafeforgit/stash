import { Progress as ProgressPrimitive } from "@base-ui/react/progress";

import { cn } from "@/lib/utils";

function Progress({
  className,
  children,
  value,
  ...props
}: ProgressPrimitive.Root.Props) {
  return (
    <ProgressPrimitive.Root
      value={value}
      data-slot="progress"
      className={cn("flex flex-wrap gap-3", className)}
      {...props}
    >
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
    </ProgressPrimitive.Root>
  );
}

function ProgressTrack({ className, ...props }: ProgressPrimitive.Track.Props) {
  return (
    <ProgressPrimitive.Track
      className={cn(
        "relative flex h-1 w-full items-center overflow-x-hidden rounded-full bg-muted",
        className,
      )}
      data-slot="progress-track"
      {...props}
    />
  );
}

function ProgressIndicator({
  className,
  ...props
}: ProgressPrimitive.Indicator.Props) {
  return (
    <ProgressPrimitive.Indicator
      data-slot="progress-indicator"
      // `transition-[width]` not `transition-all`: Base UI's Indicator
      // sets inline `width: N%` from the value, but also applies
      // `data-progressing` / `data-complete` / `data-indeterminate`
      // attributes that stylistically may shift other properties (e.g.
      // opacity in custom themes). Transitioning *all* properties on
      // every value tick visibly stalls at the end of high-frequency
      // updates (download progress fires several times per second);
      // pinning the transition to width keeps the bar smooth and lets
      // attribute-driven property changes apply instantly.
      className={cn(
        "h-full bg-primary transition-[width] duration-150 ease-linear",
        className,
      )}
      {...props}
    />
  );
}

function ProgressLabel({ className, ...props }: ProgressPrimitive.Label.Props) {
  return (
    <ProgressPrimitive.Label
      className={cn("text-sm font-medium", className)}
      data-slot="progress-label"
      {...props}
    />
  );
}

function ProgressValue({ className, ...props }: ProgressPrimitive.Value.Props) {
  return (
    <ProgressPrimitive.Value
      className={cn(
        "ml-auto text-sm text-muted-foreground tabular-nums",
        className,
      )}
      data-slot="progress-value"
      {...props}
    />
  );
}

export {
  Progress,
  ProgressTrack,
  ProgressIndicator,
  ProgressLabel,
  ProgressValue,
};
