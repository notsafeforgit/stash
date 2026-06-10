import { useState } from "react";
import { Button } from "src/components/ui/button";
import { DirectorySelectionDialog } from "src/components/shared/directory-selection-dialog";

interface IProps {
  /** Button label (typically a localized "Selective X…" message). */
  buttonLabel: React.ReactNode;
  /** Button variant — defaults to outline; pass "destructive" for clean. */
  buttonVariant?: React.ComponentProps<typeof Button>["variant"];
  /** Dialog title. */
  dialogTitle: React.ReactNode;
  /** Optional dialog description (warning text, dry-run note, etc.). */
  dialogDescription?: React.ReactNode;
  /** Extra slot below the folder picker (e.g. preview-options override). */
  extra?: React.ReactNode;
  /** Variant of the dialog's confirm button. */
  confirmVariant?: React.ComponentProps<typeof Button>["variant"];
  /** Confirm-button label inside the dialog (defaults to "Confirm"). */
  confirmText?: React.ReactNode;
  /** Called with the chosen paths after the user confirms. */
  onConfirm: (paths: string[]) => void;
  /** If true, allow confirming with zero paths (defaults to false). */
  allowEmpty?: boolean;
}

/**
 * Button + DirectorySelectionDialog combo, owning its own open state. Used
 * by every Selective X entry point on the Tasks page so each call site is
 * just a tag-soup-free three-prop component.
 */
export function SelectivePathsButton({
  buttonLabel,
  buttonVariant = "outline",
  dialogTitle,
  dialogDescription,
  extra,
  confirmVariant = "default",
  confirmText,
  onConfirm,
  allowEmpty,
}: IProps) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button
        type="button"
        variant={buttonVariant}
        onClick={() => setOpen(true)}
      >
        {buttonLabel}…
      </Button>
      <DirectorySelectionDialog
        open={open}
        onOpenChange={setOpen}
        title={dialogTitle}
        description={dialogDescription}
        extra={extra}
        confirmVariant={confirmVariant}
        confirmText={confirmText}
        allowEmpty={allowEmpty}
        onConfirm={(paths) => {
          setOpen(false);
          onConfirm(paths);
        }}
      />
    </>
  );
}
