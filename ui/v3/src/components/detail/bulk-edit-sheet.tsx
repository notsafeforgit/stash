import type React from "react";
import { useIntl } from "react-intl";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetFooter,
} from "src/components/ui/sheet";
import { Button } from "src/components/ui/button";
import { Spinner } from "src/components/ui/spinner";
import { Switch } from "src/components/ui/switch";
import { Field, FieldTitle } from "src/components/ui/field";
import type { BulkApplyTarget } from "src/components/list/list-provider";

// ── Types ─────────────────────────────────────────────────────────────────────

export interface BulkEditSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  saving: boolean;
  onSubmit: () => void;
  /** When provided and totalCount > itemCount, shows the "apply to all" toggle. */
  applyToAllTarget?: BulkApplyTarget;
  /** Total items matching the current list filter. */
  totalCount?: number;
  /** Number of currently selected items. */
  itemCount: number;
  applyToAll: boolean;
  onApplyToAllChange: (v: boolean) => void;
  children: React.ReactNode;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function BulkEditSheet({
  open,
  onOpenChange,
  title,
  saving,
  onSubmit,
  applyToAllTarget,
  totalCount,
  itemCount,
  applyToAll,
  onApplyToAllChange,
  children,
}: BulkEditSheetProps) {
  const intl = useIntl();

  const showApplyToAll =
    !!applyToAllTarget && totalCount !== undefined && totalCount > itemCount;

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex flex-col gap-0 p-0 sm:max-w-md"
      >
        <SheetHeader className="border-b px-4 py-3">
          <SheetTitle>{title}</SheetTitle>
        </SheetHeader>

        <form
          className="flex flex-col flex-1 overflow-hidden"
          onSubmit={(e) => {
            e.preventDefault();
            onSubmit();
          }}
        >
          {/* `overflow-x-hidden` + `overscroll-contain` keeps both axes
              pinned to this scroll container:
              - Horizontal: prevents the browser's wheel-axis lock from
                sticking to horizontal when a trackpad gesture starts with
                even a tiny sideways component.
              - Vertical: stops at-boundary overscroll from chaining up
                into Base UI's body scroll lock (which would silently
                swallow the gesture and skip the rubberband); rubberband
                now renders consistently for both momentum and direct
                push gestures. */}
          <div className="flex-1 overflow-y-auto overflow-x-hidden overscroll-contain px-4 py-4 flex flex-col gap-4">
            {children}
          </div>

          {showApplyToAll && (
            <div className="shrink-0 border-t bg-popover px-4 py-3">
              <Field orientation="horizontal">
                <FieldTitle>
                  {intl.formatMessage(
                    {
                      id: "dialogs.bulk_edit.apply_to_all",
                      defaultMessage: "Apply to all {count} matching",
                    },
                    { count: totalCount },
                  )}
                </FieldTitle>
                <Switch
                  checked={applyToAll}
                  onCheckedChange={onApplyToAllChange}
                  disabled={saving}
                  size="sm"
                />
              </Field>
            </div>
          )}

          <SheetFooter className="border-t px-4 py-3 flex-row justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={saving}
              onClick={() => onOpenChange(false)}
            >
              {intl.formatMessage({
                id: "actions.cancel",
                defaultMessage: "Cancel",
              })}
            </Button>
            <Button type="submit" size="sm" disabled={saving}>
              {saving && <Spinner className="size-4" />}
              {intl.formatMessage({
                id: "actions.save",
                defaultMessage: "Save",
              })}
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}
