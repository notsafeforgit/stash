import type React from "react";
import { useRef } from "react";
import { X } from "lucide-react";
import { useIntl } from "react-intl";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group";

export interface IClearableInput {
  className?: string;
  value: string;
  setValue: (value: string) => void;
  onEnter?: () => void;
  placeholder?: string;
  focus?: readonly [
    React.RefObject<HTMLInputElement | null>,
    (selectAll?: boolean) => void,
  ];
}

export const ClearableInput: React.FC<IClearableInput> = ({
  className,
  value,
  setValue,
  onEnter,
  placeholder,
  focus,
}) => {
  const intl = useIntl();
  const ownRef = useRef<HTMLInputElement>(null);
  const inputRef = focus ? focus[0] : ownRef;
  const queryClearShowing = !!value;

  function onClearQuery() {
    setValue("");
    inputRef.current?.focus();
  }

  function onInputKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Escape") {
      inputRef.current?.blur();
    }
    if (e.key === "Enter" && onEnter) {
      onEnter();
    }
  }

  return (
    <InputGroup className={className}>
      <InputGroupInput
        ref={inputRef}
        type="text"
        placeholder={placeholder}
        aria-label={placeholder ?? intl.formatMessage({ id: "actions.search" })}
        value={value}
        onChange={(e) => setValue(e.currentTarget.value)}
        onKeyDown={onInputKeyDown}
      />
      {queryClearShowing && (
        <InputGroupAddon align="inline-end">
          <InputGroupButton
            type="button"
            size="icon-xs"
            onClick={onClearQuery}
            title={intl.formatMessage({ id: "actions.clear" })}
            aria-label={intl.formatMessage({ id: "actions.clear" })}
          >
            <X />
          </InputGroupButton>
        </InputGroupAddon>
      )}
    </InputGroup>
  );
};

export default ClearableInput;
