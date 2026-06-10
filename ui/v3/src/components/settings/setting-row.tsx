/**
 * Shared building blocks for the Settings pages. Each row renders a
 * label + optional description on the left and a control on the right,
 * using the shadcn Field primitives, and writes through the supplied
 * `onChange` immediately (switch / select) or on commit (text / number /
 * list inputs commit on blur or Enter). Persistence feedback comes from
 * the global save-indicator, so rows don't render their own spinners.
 */
import { useId, useState } from "react";
import { Minus, Plus } from "lucide-react";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import { Switch } from "src/components/ui/switch";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "src/components/ui/field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";

export function SettingsSection({
  title,
  description,
  children,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <FieldSet>
      <FieldLegend>{title}</FieldLegend>
      {description && <FieldDescription>{description}</FieldDescription>}
      <FieldGroup>{children}</FieldGroup>
    </FieldSet>
  );
}

interface RowProps {
  label: React.ReactNode;
  description?: React.ReactNode;
  disabled?: boolean;
}

export function SettingSwitch({
  label,
  description,
  disabled,
  checked,
  onChange,
}: RowProps & {
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  const id = useId();
  return (
    <Field orientation="horizontal">
      <FieldContent>
        <FieldLabel htmlFor={id}>{label}</FieldLabel>
        {description && <FieldDescription>{description}</FieldDescription>}
      </FieldContent>
      <Switch
        id={id}
        checked={checked}
        onCheckedChange={onChange}
        disabled={disabled}
      />
    </Field>
  );
}

export function SettingSelect({
  label,
  description,
  disabled,
  value,
  options,
  onChange,
  triggerClassName = "w-44",
}: RowProps & {
  value: string;
  options: { value: string; label: string }[];
  onChange: (v: string) => void;
  triggerClassName?: string;
}) {
  const id = useId();
  const current = options.find((o) => o.value === value);
  return (
    <Field orientation="horizontal">
      <FieldContent>
        <FieldLabel htmlFor={id}>{label}</FieldLabel>
        {description && <FieldDescription>{description}</FieldDescription>}
      </FieldContent>
      <Select
        value={value}
        onValueChange={(v) => {
          if (v !== null && v !== value) onChange(v);
        }}
        disabled={disabled}
      >
        <SelectTrigger id={id} className={triggerClassName}>
          <SelectValue>{current?.label ?? value}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          {options.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  );
}

/**
 * String setting committed on blur / Enter. Local draft state keeps
 * typing responsive; the saved value re-syncs whenever the upstream
 * value changes (render-time adjustment keyed on `value`).
 */
export function SettingText({
  label,
  description,
  disabled,
  value,
  onChange,
  placeholder,
  type = "text",
  inputClassName = "w-64 max-w-full",
}: RowProps & {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  type?: string;
  inputClassName?: string;
}) {
  const id = useId();
  const [draft, setDraft] = useState(value);
  const [syncedValue, setSyncedValue] = useState(value);
  if (syncedValue !== value) {
    setSyncedValue(value);
    setDraft(value);
  }

  function commit() {
    if (draft !== value) onChange(draft);
  }

  return (
    <Field orientation="responsive">
      <FieldContent>
        <FieldLabel htmlFor={id}>{label}</FieldLabel>
        {description && <FieldDescription>{description}</FieldDescription>}
      </FieldContent>
      <Input
        id={id}
        type={type}
        value={draft}
        placeholder={placeholder}
        disabled={disabled}
        className={inputClassName}
        onChange={(e) => setDraft(e.currentTarget.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") commit();
        }}
      />
    </Field>
  );
}

/** Number setting committed on blur / Enter. Empty input commits 0. */
export function SettingNumber({
  label,
  description,
  disabled,
  value,
  onChange,
  inputClassName = "w-28",
}: RowProps & {
  value: number;
  onChange: (v: number) => void;
  inputClassName?: string;
}) {
  const id = useId();
  const [draft, setDraft] = useState(String(value));
  const [syncedValue, setSyncedValue] = useState(value);
  if (syncedValue !== value) {
    setSyncedValue(value);
    setDraft(String(value));
  }

  function commit() {
    const parsed = Number(draft);
    const next = Number.isFinite(parsed) ? parsed : 0;
    if (next !== value) onChange(next);
  }

  return (
    <Field orientation="horizontal">
      <FieldContent>
        <FieldLabel htmlFor={id}>{label}</FieldLabel>
        {description && <FieldDescription>{description}</FieldDescription>}
      </FieldContent>
      <Input
        id={id}
        type="number"
        inputMode="numeric"
        value={draft}
        disabled={disabled}
        className={inputClassName}
        onChange={(e) => setDraft(e.currentTarget.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") commit();
        }}
      />
    </Field>
  );
}

/**
 * Editable string list (exclusion patterns, ffmpeg args, IP whitelists).
 * Each entry commits on blur / Enter; the add row appends `defaultNewValue`
 * (or an empty string) for immediate editing.
 */
export function SettingStringList({
  label,
  description,
  disabled,
  value,
  onChange,
  defaultNewValue = "",
}: RowProps & {
  value: string[];
  onChange: (v: string[]) => void;
  defaultNewValue?: string;
}) {
  const id = useId();

  function setEntry(index: number, entry: string) {
    const next = value.slice();
    next[index] = entry;
    onChange(next);
  }

  function removeEntry(index: number) {
    onChange(value.filter((_, i) => i !== index));
  }

  return (
    <Field>
      <FieldContent>
        <FieldLabel htmlFor={`${id}-add`}>{label}</FieldLabel>
        {description && <FieldDescription>{description}</FieldDescription>}
      </FieldContent>
      <div className="space-y-2">
        {value.map((entry, i) => (
          <ListEntryInput
            // Positional identity: entries have no stable ids and may
            // repeat; reorder is not supported here.
            key={i}
            value={entry}
            disabled={disabled}
            onCommit={(v) => setEntry(i, v)}
            onRemove={() => removeEntry(i)}
          />
        ))}
        <Button
          id={`${id}-add`}
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled}
          onClick={() => onChange([...value, defaultNewValue])}
        >
          <Plus className="size-4" />
          Add
        </Button>
      </div>
    </Field>
  );
}

function ListEntryInput({
  value,
  disabled,
  onCommit,
  onRemove,
}: {
  value: string;
  disabled?: boolean;
  onCommit: (v: string) => void;
  onRemove: () => void;
}) {
  const [draft, setDraft] = useState(value);
  const [syncedValue, setSyncedValue] = useState(value);
  if (syncedValue !== value) {
    setSyncedValue(value);
    setDraft(value);
  }

  function commit() {
    if (draft !== value) onCommit(draft);
  }

  return (
    <div className="flex items-center gap-2">
      <Input
        value={draft}
        disabled={disabled}
        className="w-80 max-w-full font-mono text-sm"
        onChange={(e) => setDraft(e.currentTarget.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") commit();
        }}
      />
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label="Remove"
        disabled={disabled}
        onClick={onRemove}
      >
        <Minus className="size-4" />
      </Button>
    </div>
  );
}

/** Static display row (API key, version info) with optional action slot. */
export function SettingDisplay({
  label,
  description,
  value,
  actions,
}: {
  label: React.ReactNode;
  description?: React.ReactNode;
  value?: React.ReactNode;
  actions?: React.ReactNode;
}) {
  return (
    <Field orientation="responsive">
      <FieldContent>
        <FieldLabel>{label}</FieldLabel>
        {value != null && value !== "" && (
          <div className="text-sm break-all text-muted-foreground">{value}</div>
        )}
        {description && <FieldDescription>{description}</FieldDescription>}
      </FieldContent>
      {actions && (
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          {actions}
        </div>
      )}
    </Field>
  );
}
