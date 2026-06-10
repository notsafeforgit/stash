import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { cn } from "src/lib/utils";
import { Button } from "src/components/ui/button";
import { Checkbox } from "src/components/ui/checkbox";
import { Label } from "src/components/ui/label";

export function TaskSectionHeading({
  title,
  description,
  actions,
  children,
  defaultOpen = false,
  collapsible = false,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  children?: React.ReactNode;
  defaultOpen?: boolean;
  collapsible?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  const hasOptions = !!children;

  return (
    <div className="space-y-3 border-b py-4 last:border-b-0">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="text-sm font-medium">{title}</div>
          {description && (
            <div className="text-sm text-muted-foreground">{description}</div>
          )}
        </div>
        {actions && (
          <div className="flex flex-wrap items-center gap-2">{actions}</div>
        )}
      </div>
      {collapsible && hasOptions && (
        <Button
          type="button"
          variant="ghost"
          size="xs"
          onClick={() => setOpen((v) => !v)}
          className="self-start px-1 text-xs font-medium text-muted-foreground hover:text-foreground"
        >
          {open ? (
            <ChevronDown className="size-3" />
          ) : (
            <ChevronRight className="size-3" />
          )}
          <span>{open ? "Hide options" : "Show options"}</span>
        </Button>
      )}
      {hasOptions && (collapsible ? open : true) && (
        <div className="space-y-2 pl-1">{children}</div>
      )}
    </div>
  );
}

export function TaskOptionToggle({
  id,
  label,
  description,
  checked,
  onChange,
  disabled,
  className,
}: {
  id: string;
  label: React.ReactNode;
  description?: React.ReactNode;
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  className?: string;
}) {
  return (
    <div className={cn("flex items-start gap-2", className)}>
      <Checkbox
        id={id}
        checked={checked}
        onCheckedChange={(v) => onChange(v === true)}
        disabled={disabled}
        className="mt-0.5"
      />
      <div className="min-w-0 flex-1">
        <Label
          htmlFor={id}
          className={cn("block text-sm leading-snug", disabled && "opacity-50")}
        >
          {label}
        </Label>
        {description && (
          <div className="text-xs text-muted-foreground">{description}</div>
        )}
      </div>
    </div>
  );
}

export function TaskGroup({
  title,
  children,
}: {
  title: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="rounded-lg border bg-card">
      <div className="border-b px-4 py-3">
        <h2 className="text-base font-medium">{title}</h2>
      </div>
      <div className="px-4">{children}</div>
    </section>
  );
}
