import type { ReactNode } from "react";
import { ChevronLeft, X } from "lucide-react";
import { useIntl } from "react-intl";
import { Button } from "@/components/ui/button";

/** The form keeps its own scrolling fields and Save/Discard action bar. */
export function DetailEditorLayout({
  title,
  onClose,
  children,
}: {
  title: ReactNode;
  onClose: () => void;
  children: ReactNode;
}) {
  const intl = useIntl();
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-1 border-b border-border px-3 py-2 md:px-1 md:py-1">
        <Button
          variant="ghost"
          size="sm"
          className="hidden md:inline-flex shrink-0 px-2"
          onClick={onClose}
          aria-label={intl.formatMessage({ id: "actions.back" })}
        >
          <ChevronLeft />
        </Button>
        <h2 className="min-w-0 truncate text-base font-semibold leading-tight">
          {title}
        </h2>
      </div>
      <div className="min-h-0 flex-1">{children}</div>
      <div className="md:hidden flex shrink-0 justify-end border-t px-3 py-1 pb-[max(0.25rem,env(safe-area-inset-bottom,0px))]">
        <Button variant="ghost" size="lg" className="h-11" onClick={onClose}>
          <X data-icon="inline-start" />
          {intl.formatMessage({ id: "actions.close" })}
        </Button>
      </div>
    </div>
  );
}
