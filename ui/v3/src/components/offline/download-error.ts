import type { IntlShape } from "react-intl";
import { VIDEO_PROCESSING_FAILED } from "./download-processing";

export function downloadErrorMessage(
  intl: IntlShape,
  error: string | undefined,
) {
  if (error === VIDEO_PROCESSING_FAILED)
    return intl.formatMessage({ id: "offline.errors.processing_failed" });
  return error ?? intl.formatMessage({ id: "offline.card.error_unknown" });
}
