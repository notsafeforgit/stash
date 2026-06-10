import type React from "react";
import { useRef, useState } from "react";
import { useIntl } from "react-intl";
import {
  ImageIcon,
  FolderOpenIcon,
  LinkIcon,
  ClipboardIcon,
  XIcon,
  CheckIcon,
} from "lucide-react";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "src/components/ui/dropdown-menu";
import { toast } from "sonner";
import ImageUtils from "src/utils/image";
import { errorToString } from "src/utils/errors";

export interface ImageInputProps {
  value: string | null;
  onChange: (value: string | null) => void;
  disabled?: boolean;
  /** Locale id for the set-image button label. Defaults to "actions.set_image". */
  setLabelId?: string;
  /** Locale id for the clear button label. Defaults to "actions.clear_image". */
  clearLabelId?: string;
}

export function ImageInput({
  value,
  onChange,
  disabled,
  setLabelId = "actions.set_image",
  clearLabelId = "actions.clear_image",
}: ImageInputProps) {
  const intl = useIntl();
  const fileRef = useRef<React.ComponentRef<typeof Input>>(null);
  const [urlMode, setUrlMode] = useState(false);
  const [urlValue, setUrlValue] = useState("");

  function handleFile(e: React.ChangeEvent<HTMLInputElement>) {
    ImageUtils.onImageChange(e, (data) => onChange(data));
    // reset so the same file can be re-selected if needed
    if (fileRef.current) (fileRef.current as HTMLInputElement).value = "";
  }

  async function handleClipboard() {
    try {
      const data = await ImageUtils.readClipboardImage();
      if (data) {
        onChange(data);
        toast.success(
          intl.formatMessage({
            id: "toasts.clipboard_image_pasted",
            defaultMessage: "Image pasted from clipboard",
          }),
        );
      } else {
        toast.error(
          intl.formatMessage({
            id: "toasts.clipboard_no_image",
            defaultMessage: "No image found in clipboard",
          }),
        );
      }
    } catch (e) {
      toast.error(errorToString(e));
    }
  }

  async function handleUrlConfirm() {
    const url = urlValue.trim();
    if (!url) return;
    try {
      const data = await ImageUtils.imageToDataURL(url);
      onChange(data);
      setUrlMode(false);
      setUrlValue("");
    } catch (e) {
      toast.error(errorToString(e));
    }
  }

  function cancelUrl() {
    setUrlMode(false);
    setUrlValue("");
  }

  const canPasteClipboard = window.isSecureContext;

  return (
    <div className="flex flex-col gap-2">
      {/* Thumbnail */}
      {value && (
        <img
          src={value}
          alt=""
          className="max-h-40 max-w-full rounded border border-border object-contain self-start"
        />
      )}

      {/* URL input row — shown when urlMode is active */}
      {urlMode && (
        <div className="flex items-center gap-2">
          <Input
            value={urlValue}
            onChange={(e) => setUrlValue(e.target.value)}
            placeholder="https://…"
            autoFocus
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                handleUrlConfirm();
              }
              if (e.key === "Escape") cancelUrl();
            }}
          />
          <Button
            type="button"
            size="icon"
            variant="outline"
            disabled={!urlValue.trim()}
            onClick={handleUrlConfirm}
            aria-label={intl.formatMessage({
              id: "actions.confirm",
              defaultMessage: "OK",
            })}
          >
            <CheckIcon className="size-4" />
          </Button>
          <Button
            type="button"
            size="icon"
            variant="ghost"
            onClick={cancelUrl}
            aria-label={intl.formatMessage({
              id: "actions.cancel",
              defaultMessage: "Cancel",
            })}
          >
            <XIcon className="size-4" />
          </Button>
        </div>
      )}

      {/* Controls */}
      {!urlMode && (
        <div className="flex flex-wrap items-center gap-2">
          {/* Set image dropdown */}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={disabled}
                />
              }
            >
              <ImageIcon className="size-3.5" />
              {intl.formatMessage({
                id: setLabelId,
                defaultMessage: "Set image…",
              })}
            </DropdownMenuTrigger>
            <DropdownMenuContent>
              {/* From file */}
              <DropdownMenuItem
                disabled={disabled}
                onClick={() => fileRef.current?.click()}
              >
                <FolderOpenIcon className="size-4" />
                {intl.formatMessage({
                  id: "actions.from_file",
                  defaultMessage: "From file…",
                })}
              </DropdownMenuItem>

              {/* From URL */}
              <DropdownMenuItem
                disabled={disabled}
                onClick={() => setUrlMode(true)}
              >
                <LinkIcon className="size-4" />
                {intl.formatMessage({
                  id: "actions.from_url",
                  defaultMessage: "From URL…",
                })}
              </DropdownMenuItem>

              {/* From clipboard */}
              {canPasteClipboard && (
                <DropdownMenuItem disabled={disabled} onClick={handleClipboard}>
                  <ClipboardIcon className="size-4" />
                  {intl.formatMessage({
                    id: "actions.from_clipboard",
                    defaultMessage: "From clipboard",
                  })}
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>

          {/* Clear button — only shown when an image is set */}
          {value && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={disabled}
              onClick={() => onChange(null)}
            >
              <XIcon className="size-3.5" />
              {intl.formatMessage({
                id: clearLabelId,
                defaultMessage: "Clear image",
              })}
            </Button>
          )}
        </div>
      )}

      {/* Hidden file input */}
      <Input
        ref={fileRef}
        type="file"
        accept="image/*"
        className="sr-only"
        onChange={handleFile}
        disabled={disabled}
        tabIndex={-1}
        aria-hidden="true"
      />
    </div>
  );
}
