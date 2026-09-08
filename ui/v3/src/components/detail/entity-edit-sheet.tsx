import type { ReactNode } from "react";
import { useIntl } from "react-intl";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Spinner } from "@/components/ui/spinner";

interface EntityEditSheetProps {
  open: boolean;
  onClose: () => void;
  entityType: string;
  loading: boolean;
  children: ReactNode;
}

/** Keeps dismissal visible while the entity form owns its scrolling fields. */
export function EntityEditSheet({
  open,
  onClose,
  entityType,
  loading,
  children,
}: EntityEditSheetProps) {
  const intl = useIntl();
  const closeLabel = intl.formatMessage({ id: "actions.close" });

  return (
    <Sheet
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) onClose();
      }}
    >
      <SheetContent
        side="right"
        className="gap-0 p-0 sm:max-w-xl"
        showCloseButton={false}
      >
        <SheetHeader className="shrink-0 flex-row items-center justify-between gap-2 border-b px-3 py-2">
          <SheetTitle className="min-w-0">
            {intl.formatMessage(
              { id: "actions.edit_entity" },
              { entityType: entityType.toLocaleLowerCase() },
            )}
          </SheetTitle>
          <SheetClose
            render={
              <Button
                variant="ghost"
                size="icon"
                className="shrink-0"
                aria-label={closeLabel}
                title={closeLabel}
              />
            }
          >
            <X />
          </SheetClose>
        </SheetHeader>
        <div className="min-h-0 flex-1 overflow-hidden">
          {loading && (
            <div className="flex items-center justify-center p-8">
              <Spinner />
            </div>
          )}
          {children}
        </div>
      </SheetContent>
    </Sheet>
  );
}
