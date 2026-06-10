import { useRef } from "react";
import { PlusIcon, Trash2Icon } from "lucide-react";
import { Button } from "src/components/ui/button";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "src/components/ui/input-group";

interface UrlListFieldProps {
  value: string[];
  onChange: (urls: string[]) => void;
  disabled?: boolean;
  placeholder?: string;
}

let urlRowIdCounter = 0;
const makeRowId = () => `url-row-${++urlRowIdCounter}`;

export function UrlListField({
  value,
  onChange,
  disabled = false,
  placeholder = "https://…",
}: UrlListFieldProps) {
  // Stable per-row keys. `key={i}` on the input rows lets React mismatch
  // DOM nodes when the form re-renders mid-edit (the focused input can
  // briefly bind to a sibling row), which surfaces on mobile as taps
  // landing on the wrong input. Internal mutations (add/remove) keep
  // keysRef and `value` in lockstep; on external value changes (form
  // reset, scrape result) the length-mismatch branch regenerates IDs
  // for any new tail entries.
  const keysRef = useRef<string[]>([]);
  if (keysRef.current.length !== value.length) {
    keysRef.current = value.map((_, i) => keysRef.current[i] ?? makeRowId());
  }

  function update(index: number, url: string) {
    const next = [...value];
    next[index] = url;
    onChange(next);
  }

  function remove(index: number) {
    keysRef.current = keysRef.current.filter((_, i) => i !== index);
    onChange(value.filter((_, i) => i !== index));
  }

  function add() {
    keysRef.current = [...keysRef.current, makeRowId()];
    onChange([...value, ""]);
  }

  return (
    <div className="flex flex-col gap-1.5">
      {value.map((url, i) => (
        <InputGroup key={keysRef.current[i]}>
          <InputGroupInput
            value={url}
            placeholder={placeholder}
            disabled={disabled}
            onChange={(e) => update(i, e.target.value)}
          />
          <InputGroupAddon align="inline-end">
            <InputGroupButton
              size="icon-xs"
              variant="ghost"
              disabled={disabled}
              aria-label="Remove URL"
              onClick={() => remove(i)}
            >
              <Trash2Icon className="pointer-events-none size-3.5" />
            </InputGroupButton>
          </InputGroupAddon>
        </InputGroup>
      ))}
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={disabled}
        className="w-fit"
        onClick={add}
      >
        <PlusIcon className="size-3.5" />
        Add URL
      </Button>
    </div>
  );
}
