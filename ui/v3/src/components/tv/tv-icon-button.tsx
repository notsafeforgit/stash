import type { ComponentProps } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/** Keep a comfortable hit target while only painting the icon over the video. */
export function TvIconButton({
  className,
  ...props
}: Omit<ComponentProps<typeof Button>, "variant" | "size">) {
  return (
    <Button
      variant="transparent"
      size="icon-lg"
      className={cn(
        "pointer-events-auto size-11 rounded-full text-white hover:text-white/80 focus-visible:border-white/80 focus-visible:ring-white/60 [&_svg:not([class*='size-'])]:size-6 [&_svg]:drop-shadow-[0_1px_2px_rgb(0_0_0/0.85)]",
        className,
      )}
      {...props}
    />
  );
}
