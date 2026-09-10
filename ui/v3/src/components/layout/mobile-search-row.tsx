import type { ReactNode, RefObject } from "react";
import { ChevronRight } from "lucide-react";
import { useIntl } from "react-intl";
import { Button } from "@/components/ui/button";

export function MobileSearchRow({
  rowRef,
  onClose,
  children,
}: {
  rowRef: RefObject<HTMLDivElement | null>;
  onClose: () => void;
  children: ReactNode;
}) {
  const intl = useIntl();
  return (
    <div
      ref={rowRef}
      data-mobile-search-row=""
      className="flex h-11 min-w-0 flex-1 items-center gap-[var(--mobile-toolbar-gap)]"
    >
      {children}
      <Button
        variant="ghost"
        size="icon-lg"
        className="size-11 shrink-0"
        onClick={onClose}
        aria-label={intl.formatMessage({ id: "actions.close_search" })}
      >
        <ChevronRight />
      </Button>
    </div>
  );
}
