/**
 * Shared building blocks for the Settings pages. Each row renders a
 * label + optional description on the left and a control on the right,
 * using the shadcn Field primitives, and writes through the supplied
 * `onChange` immediately (switch / select) or on commit (text / number /
 * list inputs commit on blur or Enter). Persistence feedback comes from
 * the global save-indicator, so rows don't render their own spinners.
 */
import { useCallback, useEffect, useId, useRef, useState } from "react";
import { useIntl } from "react-intl";
import { FolderSearch, Minus, Plus } from "lucide-react";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import { Switch } from "src/components/ui/switch";
import { FolderPickerDialog } from "src/components/shared/folder-picker-dialog";
import { DestructiveConfirmDialog } from "src/components/shared/destructive-confirm-dialog";
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

/**
 * Draft state for commit-on-blur/Enter inputs:
 *  - keeps typing responsive in local state;
 *  - re-syncs whenever the saved value changes (render-time adjustment
 *    keyed on `value`);
 *  - `commit()` invokes `onCommit(draft)` only when the draft differs
 *    from the saved value;
 *  - flushes a pending edit on unmount, so navigating away without
 *    blurring doesn't silently drop the change. (Rows that gate commits
 *    behind a confirm dialog pass an `onCommit` that only opens the
 *    dialog — on unmount that's a state update on an unmounting
 *    component, i.e. a no-op, so dangerous edits are dropped rather
 *    than silently applied.)
 */
function useDraftValue(value: string, onCommit: (draft: string) => void) {
  const [draft, setDraft] = useState(value);
  const [syncedValue, setSyncedValue] = useState(value);
  if (syncedValue !== value) {
    setSyncedValue(value);
    setDraft(value);
  }

  // Refs so commit/flush read current state without re-subscribing the
  // unmount effect on every keystroke.
  const latest = useRef({ draft, value, onCommit });
  latest.current = { draft, value, onCommit };

  const commit = useCallback(() => {
    const s = latest.current;
    if (s.draft !== s.value) s.onCommit(s.draft);
  }, []);

  useEffect(
    () => () => {
      const s = latest.current;
      if (s.draft !== s.value) s.onCommit(s.draft);
    },
    [],
  );

  return { draft, setDraft, commit };
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

/**
 * `confirm` gates changes behind a confirmation dialog showing the given
 * message — for selects whose change has consequences beyond the setting
 * itself (blob storage, hash algorithm). Cancelling keeps the saved
 * value (the select is controlled, so it snaps back by itself).
 */
export function SettingSelect({
  label,
  description,
  disabled,
  value,
  options,
  onChange,
  triggerClassName = "w-44",
  confirm,
}: RowProps & {
  value: string;
  options: { value: string; label: string }[];
  onChange: (v: string) => void;
  triggerClassName?: string;
  confirm?: React.ReactNode;
}) {
  const id = useId();
  const [confirmingValue, setConfirmingValue] = useState<string | null>(null);
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
          if (v === null || v === value) return;
          if (confirm) setConfirmingValue(v);
          else onChange(v);
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
      {confirm && confirmingValue !== null && (
        <DestructiveConfirmDialog
          open
          onOpenChange={(o) => {
            if (!o) setConfirmingValue(null);
          }}
          title={label}
          onConfirm={() => {
            onChange(confirmingValue);
            setConfirmingValue(null);
          }}
        >
          <p className="text-sm">{confirm}</p>
        </DestructiveConfirmDialog>
      )}
    </Field>
  );
}

/** String setting committed on blur / Enter (see useDraftValue). */
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
  const { draft, setDraft, commit } = useDraftValue(value, onChange);

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

/**
 * Filesystem path setting. Unlike SettingText, the input sits on its own
 * full-width line (long paths stay readable instead of scrolling inside a
 * 16rem box) in a monospace face, and an optional picker button browses
 * the server's directories via FolderPickerDialog. Commits on blur /
 * Enter / pick, like SettingText.
 *
 * Set `picker={false}` for file paths (executables, database file) where
 * a directory picker can't choose the final target.
 *
 * `confirm` gates every commit behind a confirmation dialog showing the
 * given message — for paths whose change has consequences beyond the
 * setting itself (database file, blobs directory). Cancelling reverts
 * the input to the saved value.
 */
export function SettingPath({
  label,
  description,
  disabled,
  value,
  onChange,
  placeholder,
  picker = true,
  confirm,
}: RowProps & {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  picker?: boolean;
  confirm?: React.ReactNode;
}) {
  const id = useId();
  const intl = useIntl();
  const [pickerOpen, setPickerOpen] = useState(false);
  const [confirmingValue, setConfirmingValue] = useState<string | null>(null);

  const requestChange = (v: string) => {
    if (confirm) setConfirmingValue(v);
    else onChange(v);
  };

  const { draft, setDraft, commit } = useDraftValue(value, requestChange);

  return (
    <Field>
      <FieldContent>
        <FieldLabel htmlFor={id}>{label}</FieldLabel>
        {description && <FieldDescription>{description}</FieldDescription>}
      </FieldContent>
      <div className="flex w-full items-center gap-2">
        <Input
          id={id}
          value={draft}
          placeholder={placeholder}
          disabled={disabled}
          className="font-mono text-sm"
          spellCheck={false}
          autoComplete="off"
          onChange={(e) => setDraft(e.currentTarget.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === "Enter") commit();
          }}
        />
        {picker && (
          <Button
            type="button"
            variant="outline"
            size="icon"
            disabled={disabled}
            aria-label={intl.formatMessage({
              id: "actions.browse",
              defaultMessage: "Browse…",
            })}
            onClick={() => setPickerOpen(true)}
          >
            <FolderSearch className="size-4" />
          </Button>
        )}
      </div>
      {pickerOpen && (
        <FolderPickerDialog
          open
          onOpenChange={setPickerOpen}
          initialPath={draft}
          onSelect={(p) => {
            setDraft(p);
            if (p !== value) requestChange(p);
          }}
        />
      )}
      {confirm && confirmingValue !== null && (
        <DestructiveConfirmDialog
          open
          onOpenChange={(o) => {
            if (!o) {
              setConfirmingValue(null);
              setDraft(value);
            }
          }}
          title={label}
          onConfirm={() => {
            onChange(confirmingValue);
            setConfirmingValue(null);
          }}
        >
          <p className="text-sm">{confirm}</p>
        </DestructiveConfirmDialog>
      )}
    </Field>
  );
}

/**
 * Number setting committed on blur / Enter. Empty / unparsable input
 * commits `min` (or 0). Out-of-range values are clamped into
 * [min, max] rather than rejected, and `integer` rounds — the committed
 * value is therefore always valid, and the input re-syncs to show what
 * was actually saved.
 */
export function SettingNumber({
  label,
  description,
  disabled,
  value,
  onChange,
  min,
  max,
  integer = false,
  inputClassName = "w-28",
}: RowProps & {
  value: number;
  onChange: (v: number) => void;
  min?: number;
  max?: number;
  integer?: boolean;
  inputClassName?: string;
}) {
  const id = useId();
  const { draft, setDraft, commit } = useDraftValue(String(value), (d) => {
    const parsed = Number(d);
    let next = Number.isFinite(parsed) ? parsed : (min ?? 0);
    if (integer) next = Math.round(next);
    if (min !== undefined) next = Math.max(min, next);
    if (max !== undefined) next = Math.min(max, next);
    if (next !== value) onChange(next);
    // The clamp may leave the saved value unchanged while the draft
    // shows something else (e.g. "-5" clamped to an already-saved 0) —
    // re-sync the draft to the committed value either way.
    setDraft(String(next));
  });

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
        min={min}
        max={max}
        step={integer ? 1 : undefined}
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
  const { draft, setDraft, commit } = useDraftValue(value, onCommit);

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
