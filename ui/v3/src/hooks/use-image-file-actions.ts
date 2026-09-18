import { useRef, useState } from "react";
import { useIntl } from "react-intl";
import { Copy, Download } from "lucide-react";
import { toast } from "sonner";
import type { EntityAction } from "@/components/detail/entity-actions-menu";
import {
  canCopyImage,
  copyImage,
  saveImage,
  type ImageFileSource,
} from "@/utils/image-file";

type ImageFileAction = EntityAction & { disabled: boolean };

/** Shared by image cards/rows, detail actions and both image viewers.
 * No image bytes are fetched until an action is explicitly selected. */
export function useImageFileActions(
  source: ImageFileSource | undefined,
  toasterId?: string,
): ImageFileAction[] {
  const intl = useIntl();
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);

  function run(kind: "copy" | "save") {
    if (!source || pending.current) return;
    pending.current = true;
    setBusy(true);
    // Start copying synchronously, while the browser still sees the gesture.
    const operation = kind === "copy" ? copyImage(source) : saveImage(source);
    toast.promise(operation, {
      toasterId,
      loading: intl.formatMessage(
        kind === "copy"
          ? { id: "image_file.copying", defaultMessage: "Copying image…" }
          : { id: "image_file.saving", defaultMessage: "Preparing image…" },
      ),
      success: intl.formatMessage(
        kind === "copy"
          ? { id: "image_file.copied", defaultMessage: "Image copied" }
          : {
              id: "image_file.saved",
              defaultMessage: "Image download started",
            },
      ),
      error: intl.formatMessage(
        kind === "copy"
          ? {
              id: "image_file.copy_failed",
              defaultMessage:
                "Could not copy the image. Try Save image instead.",
            }
          : {
              id: "image_file.save_failed",
              defaultMessage: "Could not save the image. Please try again.",
            },
      ),
    });
    void operation
      .finally(() => {
        pending.current = false;
        setBusy(false);
      })
      .catch(() => {});
  }

  return [
    ...(canCopyImage()
      ? [
          {
            key: "copy-image",
            icon: Copy,
            label: intl.formatMessage({
              id: "actions.copy_image",
              defaultMessage: "Copy image",
            }),
            disabled: busy || !source,
            onSelect: () => run("copy"),
          },
        ]
      : []),
    {
      key: "save-image",
      icon: Download,
      label: intl.formatMessage({
        id: "actions.save_image",
        defaultMessage: "Save image",
      }),
      disabled: busy || !source,
      onSelect: () => run("save"),
    },
  ];
}
