import { useIntl } from "react-intl";
import { XIcon } from "lucide-react";
import { Button } from "src/components/ui/button";
import { Field, FieldLabel } from "src/components/ui/field";
import { ImageInput } from "./image-input";

/**
 * A form field for an entity image that supports viewing, replacing, and
 * clearing the existing server image.
 *
 * Value semantics (matches the pattern used across all entity edit forms):
 *   null  — no pending change; show the existing server image if present
 *   ""    — pending clear; image will be removed on save
 *   other — pending new image data URL; will replace the existing image on save
 */
interface ImageFieldProps {
  label: string;
  value: string | null;
  onChange: (value: string | null) => void;
  existingImagePath?: string | null;
  disabled?: boolean;
  /** Locale id for the "set image" button when no image is currently shown. */
  setLabelId?: string;
}

export function ImageField({
  label,
  value,
  onChange,
  existingImagePath,
  disabled,
  setLabelId = "actions.set_image",
}: ImageFieldProps) {
  const intl = useIntl();

  const pendingClear = value === "";
  // image_path is always a URL; when no image is stored the server appends
  // ?default=true — treat that as "no image".
  const hasRealImage =
    !!existingImagePath && !existingImagePath.includes("default=true");
  const showExisting = value === null && hasRealImage;

  return (
    <Field>
      <FieldLabel>{label}</FieldLabel>

      {showExisting && (
        <div className="flex flex-col gap-2">
          <img
            src={existingImagePath}
            alt=""
            className="max-h-40 max-w-full rounded border border-border object-contain self-start"
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={disabled}
            className="w-fit"
            onClick={() => onChange("")}
          >
            <XIcon className="size-3.5" />
            {intl.formatMessage({
              id: "actions.remove_image",
              defaultMessage: "Remove image",
            })}
          </Button>
        </div>
      )}

      {pendingClear && (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <span>
            {intl.formatMessage({
              id: "image_will_be_removed",
              defaultMessage: "Image will be removed on save.",
            })}
          </span>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={disabled}
            onClick={() => onChange(null)}
          >
            {intl.formatMessage({ id: "actions.undo", defaultMessage: "Undo" })}
          </Button>
        </div>
      )}

      {!pendingClear && (
        <ImageInput
          value={value}
          onChange={onChange}
          disabled={disabled}
          setLabelId={showExisting ? "actions.replace_image" : setLabelId}
        />
      )}
    </Field>
  );
}
