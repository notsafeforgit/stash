import type React from "react";
import { useEffect } from "react";

export const useOnOutsideClick = (
  ref: React.RefObject<HTMLElement | null>,
  callback?: () => void,
  excludeClassName?: string,
) => {
  useEffect(() => {
    if (!callback) return;

    function handleClickOutside(event: MouseEvent) {
      if (
        ref.current &&
        event.target instanceof Node &&
        !ref.current.contains(event.target) &&
        !(
          excludeClassName &&
          (event.target as HTMLElement).closest(`.${excludeClassName}`)
        )
      ) {
        callback?.();
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
    };
  }, [ref, callback, excludeClassName]);
};
