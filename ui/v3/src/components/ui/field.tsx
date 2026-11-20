import type * as React from "react";
import {
  Field as FieldPrimitive,
  FieldDescription,
  FieldError as FieldErrorPrimitive,
  FieldGroup,
  FieldLabel as FieldLabelPrimitive,
  FieldLegend as FieldLegendPrimitive,
  FieldSet,
} from "@ui-sdk/react/field";
import { Separator } from "@ui-sdk/react/separator";
import { useMemo } from "react";

import { cn } from "@/lib/utils";

type FieldOrientation = "vertical" | "horizontal" | "responsive";

type FieldProps = React.ComponentProps<typeof FieldPrimitive> & {
  orientation?: FieldOrientation;
  "data-invalid"?: boolean | "true" | "false";
  "data-disabled"?: boolean | "true" | "false";
};

function Field({
  className,
  orientation = "vertical",
  "data-invalid": dataInvalid,
  "data-disabled": dataDisabled,
  ...props
}: FieldProps) {
  const invalid = dataInvalid === true || dataInvalid === "true";
  const disabled = dataDisabled === true || dataDisabled === "true";
  return (
    <FieldPrimitive
      data-orientation={orientation}
      data-disabled={disabled ? "true" : undefined}
      invalid={invalid || undefined}
      className={className}
      {...props}
    />
  );
}

function FieldLabel({
  htmlFor,
  render,
  ...props
}: React.ComponentProps<typeof FieldLabelPrimitive>) {
  return (
    <FieldLabelPrimitive
      render={
        render ??
        (htmlFor === undefined ? undefined : (
          // biome-ignore lint/a11y/noLabelWithoutControl: The label's text comes from the field part's children, and its control from htmlFor.
          <label htmlFor={htmlFor} />
        ))
      }
      {...props}
    />
  );
}

type FieldErrorProps = Omit<
  React.ComponentProps<typeof FieldErrorPrimitive>,
  "children"
> & {
  children?: React.ReactNode;
  errors?: Array<{ message?: string } | undefined>;
};

function FieldError({
  className,
  children,
  errors,
  match = true,
  role = "alert",
  ...props
}: FieldErrorProps) {
  const content = useMemo(() => {
    if (children) {
      return children;
    }

    if (!errors?.length) {
      return null;
    }

    const uniqueErrors = [
      ...new Map(errors.map((error) => [error?.message, error])).values(),
    ];

    if (uniqueErrors.length === 1) {
      return uniqueErrors[0]?.message;
    }

    return (
      <ul className="ml-4 flex list-disc flex-col gap-1">
        {uniqueErrors.map(
          (error, index) =>
            error?.message && <li key={index}>{error.message}</li>,
        )}
      </ul>
    );
  }, [children, errors]);

  if (!content) {
    return null;
  }

  return (
    <FieldErrorPrimitive
      match={match}
      role={role}
      className={className}
      {...props}
    >
      {content}
    </FieldErrorPrimitive>
  );
}

function FieldLegend({
  variant = "legend",
  ...props
}: React.ComponentProps<typeof FieldLegendPrimitive> & {
  variant?: "legend" | "label";
}) {
  return <FieldLegendPrimitive data-variant={variant} {...props} />;
}

function FieldContent({ className, ...props }: React.ComponentProps<"div">) {
  return <div data-slot="field-content" className={cn(className)} {...props} />;
}

function FieldTitle({ className, ...props }: React.ComponentProps<"div">) {
  return <div data-slot="field-title" className={cn(className)} {...props} />;
}

function FieldSeparator({
  children,
  className,
  ...props
}: React.ComponentProps<"div"> & {
  children?: React.ReactNode;
}) {
  return (
    <div
      data-slot="field-separator"
      data-content={!!children}
      className={cn(className)}
      {...props}
    >
      <Separator className="absolute inset-0 top-1/2" />
      {children && <span data-slot="field-separator-content">{children}</span>}
    </div>
  );
}

export {
  Field,
  FieldLabel,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLegend,
  FieldSeparator,
  FieldSet,
  FieldContent,
  FieldTitle,
};
