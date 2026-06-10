import type React from "react";
import { useEffect, useRef, useState } from "react";
import { Minus, Plus } from "lucide-react";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";

export const NumberInput: React.FC<{
  value: number;
  onChange: (v: number) => void;
  min?: number;
  max?: number;
  autoFocus?: boolean;
  className?: string;
  inputClassName?: string;
}> = ({
  value,
  onChange,
  min = 0,
  max,
  autoFocus,
  className,
  inputClassName,
}) => {
  const [str, setStr] = useState(String(value));
  const strRef = useRef(str);
  strRef.current = str;

  useEffect(() => {
    const parsed = parseInt(strRef.current, 10);
    const strMatchesValue = Number.isNaN(parsed)
      ? value === 0
      : parsed === value;
    if (!strMatchesValue) setStr(String(value));
  }, [value]);

  const clamp = (n: number) => {
    let v = Math.max(min, n);
    if (max !== undefined) v = Math.min(max, v);
    return v;
  };

  function handleChange(e: React.ChangeEvent<HTMLInputElement>) {
    let next = e.target.value;
    if (str === "0" && next.length === 2 && next.startsWith("0")) {
      next = next.slice(1);
    }
    if (next !== "" && !/^\d+$/.test(next)) return;
    setStr(next);
    if (next !== "") onChange(parseInt(next, 10));
  }

  function handleBlur() {
    const n = parseInt(str, 10);
    const committed = clamp(Number.isNaN(n) ? 0 : n);
    onChange(committed);
    setStr(String(committed));
  }

  return (
    <div className={`flex gap-1 ${className ?? ""}`}>
      <Input
        type="text"
        inputMode="numeric"
        autoFocus={autoFocus}
        value={str}
        className={inputClassName}
        onChange={handleChange}
        onBlur={handleBlur}
        onFocus={(e) => e.currentTarget.select()}
      />
      <Button
        type="button"
        variant="outline"
        size="icon"
        tabIndex={-1}
        disabled={value <= min}
        onClick={() => onChange(clamp(value - 1))}
      >
        <Minus size={14} />
      </Button>
      <Button
        type="button"
        variant="outline"
        size="icon"
        tabIndex={-1}
        disabled={max !== undefined && value >= max}
        onClick={() => onChange(clamp(value + 1))}
      >
        <Plus size={14} />
      </Button>
    </div>
  );
};
