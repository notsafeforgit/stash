import type React from "react";
import { useState } from "react";
import { FormattedMessage, useIntl } from "react-intl";
import { ChevronRight, Trash2 } from "lucide-react";
import { cn } from "src/lib/utils";
import { Button } from "src/components/ui/button";
import { Checkbox } from "src/components/ui/checkbox";
import { Spinner } from "src/components/ui/spinner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";

// ── Types ─────────────────────────────────────────────────────────────────────

export interface DeleteOptions {
  deleteFile: boolean;
  deleteGenerated: boolean;
}

interface DeleteDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Render above YARL's z-index 9999 lightbox portal. */
  aboveLightbox?: boolean;
  /** Entity name shown quoted in the title, e.g. "My Scene" → Delete "My Scene"? */
  entityName?: string;
  /**
   * Quantity phrase shown unquoted in the title for bulk deletes,
   * e.g. "3 scenes" → Delete 3 scenes?
   * Ignored when `entityName` is also provided.
   */
  entityCountLabel?: string;
  /** Show delete-file and delete-generated checkboxes (scene / image / gallery) */
  showFileOptions?: boolean;
  /** Label for the "delete file" checkbox. Defaults to "Delete file" */
  deleteFileLabel?: string;
  /**
   * Optional content tucked behind a "Show details" disclosure — e.g. the
   * list of file paths a delete would touch. Hidden by default so the dialog
   * stays uncluttered; expands into a scrollable panel when toggled. When
   * `showFileOptions` is on, the disclosure only appears once the user has
   * checked the delete-file box, since the listed files are only meaningful
   * if files are actually being deleted.
   */
  details?: React.ReactNode;
  /** Label for the disclosure toggle. Defaults to "Show details" / "Hide details". */
  detailsLabel?: string;
  /** Called with chosen options when the user confirms. Should throw on error. */
  onConfirm: (opts: DeleteOptions) => Promise<void>;
}

// ── File list helper ──────────────────────────────────────────────────────────

interface DeleteFilesListProps {
  paths: readonly string[];
}

interface DeleteOptionRowProps {
  checked: boolean;
  disabled: boolean;
  label: string;
  onCheckedChange: (checked: boolean) => void;
}

function DeleteOptionRow({
  checked,
  disabled,
  label,
  onCheckedChange,
}: DeleteOptionRowProps) {
  function handleRowClick(event: React.MouseEvent<HTMLDivElement>) {
    if (disabled) return;

    const target = event.target;
    if (
      target instanceof Element &&
      (target.closest('[data-slot="checkbox"]') ||
        target.closest('input[type="checkbox"]'))
    ) {
      return;
    }

    onCheckedChange(!checked);
  }

  return (
    <div
      className={cn(
        "flex items-center gap-2.5 text-sm",
        disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer",
      )}
      onClick={handleRowClick}
    >
      <Checkbox
        aria-label={label}
        checked={checked}
        onCheckedChange={onCheckedChange}
        disabled={disabled}
      />
      <span>{label}</span>
    </div>
  );
}

/**
 * Renders a list of file paths suitable for the DeleteDialog `details` slot.
 * Monospace, breakable, no bullets — the slot itself supplies the scroll
 * container and border.
 */
export function DeleteFilesList({ paths }: DeleteFilesListProps) {
  return (
    <ul className="font-mono break-all select-text">
      {paths.map((p, i) => (
        <li
          key={`${i}-${p}`}
          className="border-border/50 border-b py-1.5 first:pt-0 last:border-b-0 last:pb-0"
        >
          {p}
        </li>
      ))}
    </ul>
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

export function DeleteDialog({
  open,
  onOpenChange,
  aboveLightbox = false,
  entityName,
  entityCountLabel,
  showFileOptions = false,
  deleteFileLabel,
  details,
  detailsLabel,
  onConfirm,
}: DeleteDialogProps) {
  const intl = useIntl();
  const [deleteFile, setDeleteFile] = useState(false);
  const [deleteGenerated, setDeleteGenerated] = useState(true);
  const [isDeleting, setIsDeleting] = useState(false);
  const [detailsOpen, setDetailsOpen] = useState(false);

  async function handleConfirm() {
    setIsDeleting(true);
    try {
      await onConfirm({ deleteFile, deleteGenerated });
      onOpenChange(false);
    } finally {
      setIsDeleting(false);
    }
  }

  // The entity name can be a long unbroken filename (e.g. a checksum-derived
  // basename). Use rich-text formatting to wrap the *entire* quoted name in
  // an overflow-wrap:anywhere span — that keeps the closing quote glued to
  // the last char of the filename. The trailing `?` is in the template
  // (outside the span) and may occasionally orphan to its own line at
  // certain widths; the user's accepted that as the lesser evil vs.
  // truncating the name.
  const titleNode = entityName ? (
    <FormattedMessage
      id="dialogs.delete_entity_title"
      defaultMessage="Delete <quoted>{name}</quoted>?"
      values={{
        name: entityName,
        quoted: (chunks) => (
          <span className="[overflow-wrap:anywhere]">
            &ldquo;{chunks}&rdquo;
          </span>
        ),
      }}
    />
  ) : entityCountLabel ? (
    <FormattedMessage
      id="dialogs.delete_count_title"
      defaultMessage="Delete {count}?"
      values={{ count: entityCountLabel }}
    />
  ) : (
    <FormattedMessage id="dialogs.delete_title" defaultMessage="Delete?" />
  );

  const fileLabelText =
    deleteFileLabel ??
    intl.formatMessage({
      id: "dialogs.delete_file",
      defaultMessage: "Delete file",
    });

  const layerClassName = aboveLightbox ? "z-[10000]" : undefined;

  return (
    <Dialog open={open} onOpenChange={isDeleting ? () => {} : onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className={layerClassName}
        overlayClassName={layerClassName}
      >
        <DialogHeader>
          <DialogTitle>{titleNode}</DialogTitle>
          <DialogDescription>
            {intl.formatMessage({
              id: "dialogs.delete_confirm_body",
              defaultMessage: "This action cannot be undone.",
            })}
          </DialogDescription>
        </DialogHeader>

        {showFileOptions && (
          <div className="flex flex-col gap-3">
            <DeleteOptionRow
              label={fileLabelText}
              checked={deleteFile}
              onCheckedChange={setDeleteFile}
              disabled={isDeleting}
            />
            <DeleteOptionRow
              label={intl.formatMessage({
                id: "dialogs.delete_generated",
                defaultMessage: "Delete generated supporting files",
              })}
              checked={deleteGenerated}
              onCheckedChange={setDeleteGenerated}
              disabled={isDeleting}
            />
          </div>
        )}

        {details && (!showFileOptions || deleteFile) && (
          <div className="flex flex-col gap-2">
            <Button
              variant="ghost"
              size="xs"
              className="text-muted-foreground hover:text-foreground self-start"
              aria-expanded={detailsOpen}
              onClick={() => setDetailsOpen((o) => !o)}
            >
              <ChevronRight
                className={cn(
                  "size-3 transition-transform",
                  detailsOpen && "rotate-90",
                )}
              />
              {detailsOpen
                ? intl.formatMessage({
                    id: "dialogs.delete_hide_details",
                    defaultMessage: "Hide details",
                  })
                : (detailsLabel ??
                  intl.formatMessage({
                    id: "dialogs.delete_show_details",
                    defaultMessage: "Show details",
                  }))}
            </Button>
            {detailsOpen && (
              <div className="bg-muted/30 max-h-48 overflow-y-auto rounded-md border p-2 text-xs">
                {details}
              </div>
            )}
          </div>
        )}

        <DialogFooter>
          <Button
            variant="outline"
            size="sm"
            disabled={isDeleting}
            onClick={() => onOpenChange(false)}
          >
            {intl.formatMessage({
              id: "actions.cancel",
              defaultMessage: "Cancel",
            })}
          </Button>
          <Button
            variant="destructive"
            size="sm"
            disabled={isDeleting}
            onClick={handleConfirm}
          >
            {isDeleting ? <Spinner className="size-4" /> : <Trash2 />}
            {intl.formatMessage({
              id: "actions.delete",
              defaultMessage: "Delete",
            })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
