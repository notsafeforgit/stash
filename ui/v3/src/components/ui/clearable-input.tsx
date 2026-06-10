import type React from "react";
import { useRef } from "react";
import { X } from "lucide-react";
import { useIntl } from "react-intl";
import { Input } from "src/components/ui/input";

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
    <div className={`clearable-input-group${className ? ` ${className}` : ""}`}>
      <Input
        ref={inputRef}
        type="text"
        className="clearable-text-field"
        placeholder={placeholder}
        value={value}
        onChange={(e) => setValue(e.currentTarget.value)}
        onKeyDown={onInputKeyDown}
      />
      {queryClearShowing && (
        <button
          type="button"
          onClick={onClearQuery}
          title={intl.formatMessage({ id: "actions.clear" })}
          className="clearable-text-field-clear"
        >
          <X className="icon" size={16} />
        </button>
      )}
    </div>
  );
};

export default ClearableInput;
