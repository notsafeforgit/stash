import { FormattedMessage } from "react-intl";
import { Button } from "src/components/ui/button";
import { Spinner } from "src/components/ui/spinner";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";

interface IProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Title text (e.g. "Auto tag"). */
  title: React.ReactNode;
  /** Body content — short string or a richer warning component. */
  children: React.ReactNode;
  /** Confirm-button label. Defaults to "Confirm". */
  confirmText?: React.ReactNode;
  /** Called when the user clicks the destructive confirm button. */
  onConfirm: () => void;
  busy?: boolean;
  disabled?: boolean;
}

/**
 * A confirm dialog with a destructive accept button. Used for actions whose
 * effects can't be undone — AutoTag, Clean, Full Import, etc. Body is
 * `children` so callers can pass a multi-paragraph warning component (e.g.
 * `<AutoTagWarning/>`) rather than a single line of description text.
 */
export function DestructiveConfirmDialog({
  open,
  onOpenChange,
  title,
  children,
  confirmText,
  onConfirm,
  busy = false,
  disabled = false,
}: IProps) {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!busy) onOpenChange(next);
      }}
    >
      <DialogContent showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        {children}
        <DialogFooter>
          <DialogClose
            render={
              <Button type="button" variant="outline" disabled={busy}>
                <FormattedMessage id="actions.cancel" defaultMessage="Cancel" />
              </Button>
            }
          />
          <Button
            type="button"
            variant="destructive"
            onClick={onConfirm}
            disabled={busy || disabled}
          >
            {busy && <Spinner data-icon="inline-start" />}
            {confirmText ?? (
              <FormattedMessage id="actions.confirm" defaultMessage="Confirm" />
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
