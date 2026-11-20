import { Button as ButtonPrimitive } from "@ui-sdk/react/button";
import type * as React from "react";
import { cn } from "@/lib/utils";

type ButtonVariant =
  | "default"
  | "outline"
  | "secondary"
  | "ghost"
  | "transparent"
  | "destructive"
  | "link";

type ButtonSize =
  | "default"
  | "xs"
  | "sm"
  | "lg"
  | "icon"
  | "icon-xs"
  | "icon-sm"
  | "icon-lg"
  | "icon-xl";

type ButtonProps = React.ComponentProps<typeof ButtonPrimitive> & {
  variant?: ButtonVariant;
  size?: ButtonSize;
};

const buttonBase =
  "inline-flex shrink-0 items-center justify-center gap-1.5 h-8 px-3 border rounded-[var(--ui-radius)] text-[length:var(--ui-font-size)] font-medium whitespace-nowrap cursor-pointer select-none focus-visible:outline-4 focus-visible:outline-offset-0 focus-visible:outline-[color:var(--ui-ring)] disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 has-[>[data-icon=inline-start]]:ps-2.5 has-[>[data-icon=inline-end]]:pe-2.5 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4";

const buttonVariantClasses: Record<ButtonVariant, string> = {
  default:
    "border-[color:var(--ui-primary)] bg-[color:var(--ui-primary)] text-[color:var(--ui-primary-foreground)] hover:bg-[color-mix(in_oklab,var(--ui-primary)_85%,var(--ui-background))]",
  outline:
    "border-[color:var(--ui-input)] bg-[color:var(--ui-background)] text-[color:var(--ui-foreground)] hover:bg-[color:var(--ui-muted)]",
  secondary:
    "border-transparent bg-[color:var(--ui-muted)] text-[color:var(--ui-foreground)] hover:bg-[color-mix(in_oklab,var(--ui-muted),var(--ui-foreground)_6%)]",
  ghost:
    "border-transparent bg-transparent text-[color:var(--ui-foreground)] hover:bg-[color:var(--ui-muted)] aria-expanded:bg-[color:var(--ui-muted)]",
  transparent:
    "border-transparent bg-transparent text-[color:var(--ui-foreground)]",
  destructive:
    "border-[color-mix(in_oklab,var(--ui-destructive)_30%,var(--ui-background))] bg-[color-mix(in_oklab,var(--ui-destructive)_8%,var(--ui-background))] text-[color:var(--ui-destructive)] hover:bg-[color-mix(in_oklab,var(--ui-destructive)_14%,var(--ui-background))]",
  link: "h-auto border-transparent bg-transparent px-0 text-[color:var(--ui-primary)] underline-offset-4 hover:underline",
};

const buttonSizeClasses: Record<ButtonSize, string> = {
  default: "",
  xs: "h-6 gap-1 px-2 text-xs [&_svg:not([class*='size-'])]:size-3",
  sm: "h-7 gap-1 px-2.5 [&_svg:not([class*='size-'])]:size-3.5",
  lg: "h-9",
  icon: "size-8 p-0",
  "icon-xs": "size-6 p-0 [&_svg:not([class*='size-'])]:size-3",
  "icon-sm": "size-7 p-0",
  "icon-lg": "size-9 p-0",
  "icon-xl": "size-20 p-0 [&_svg:not([class*='size-'])]:size-10",
};

/* For plugin elements that aren't a Button: the same look as Button, as classes. Host code uses Button. */
function buttonVariants({
  variant = "default",
  size = "default",
  className,
}: {
  variant?: ButtonVariant;
  size?: ButtonSize;
  className?: string;
} = {}): string {
  return cn(
    buttonBase,
    buttonSizeClasses[size],
    buttonVariantClasses[variant],
    className,
  );
}

function Button({
  variant = "default",
  size = "default",
  ...props
}: ButtonProps) {
  return (
    <ButtonPrimitive
      data-variant={variant === "default" ? "solid" : variant}
      data-size={size === "default" ? undefined : size}
      {...props}
    />
  );
}

export {
  Button,
  buttonVariants,
  type ButtonProps,
  type ButtonSize,
  type ButtonVariant,
};
