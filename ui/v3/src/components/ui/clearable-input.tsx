import type React from "react";
import { useRef } from "react";
import { InputClear } from "@ui-sdk/react/input";
import { X } from "lucide-react";
import { useIntl } from "react-intl";
import { InputGroup, InputGroupInput } from "@/components/ui/input-group";

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
      <InputClear
        aria-label={intl.formatMessage({ id: "actions.clear" })}
        title={intl.formatMessage({ id: "actions.clear" })}
      >
        <X aria-hidden="true" />
      </InputClear>
    </InputGroup>
  );
};

export default ClearableInput;
