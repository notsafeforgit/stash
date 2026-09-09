import type { ComponentProps } from "react";
import type { MobileNavSheet as AppMobileNavSheet } from "@/components/layout/mobile-nav-sheet";
import type { useDefaultFilterActions as appUseDefaultFilterActions } from "@/hooks/default-filter";
import { Button } from "@/components/ui/button";
import {
  BottomSheet,
  BottomSheetHeader,
  BottomSheetTitle,
} from "@/components/ui/bottom-sheet";

// Only backend-dependent integrations are substituted; toolbar, tab, form,
// popover and drawer behavior comes from the production components.
export function MobileNavSheet({
  open,
  onOpenChange,
}: ComponentProps<typeof AppMobileNavSheet>) {
  return (
    <BottomSheet open={open} onOpenChange={onOpenChange}>
      <BottomSheetHeader>
        <BottomSheetTitle>Fixture navigation</BottomSheetTitle>
      </BottomSheetHeader>
      <Button onClick={() => onOpenChange(false)}>Close navigation</Button>
    </BottomSheet>
  );
}

export function useDefaultFilterActions(): ReturnType<
  typeof appUseDefaultFilterActions
> {
  const unexpectedWrite = async () => {
    throw new Error("Default filter writes are outside this fixture's scope");
  };
  return {
    hasDefault: false,
    hasConflict: false,
    saving: false,
    setCurrent: unexpectedWrite,
    clear: unexpectedWrite,
    useLegacy: unexpectedWrite,
    keepV3: unexpectedWrite,
  };
}
