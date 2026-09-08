import { useEditableRows } from "@/hooks/use-editable-rows";
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

export function UrlListField({
  value,
  onChange,
  disabled = false,
  placeholder = "https://…",
}: UrlListFieldProps) {
  const { rows, update, remove, append } = useEditableRows(value, onChange);

  return (
    <div className="flex flex-col gap-1.5">
      {rows.map(({ key, value: url }, i) => (
        <InputGroup key={key}>
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
        onClick={() => append("")}
      >
        <PlusIcon className="size-3.5" />
        Add URL
      </Button>
    </div>
  );
}
