import { useIntl } from "react-intl";
import {
  fileSize,
  fileSizeFractionalDigits,
  formatFileSizeUnit,
} from "@/utils/file";
export function FileSize({ bytes }: { bytes: number }) {
  const intl = useIntl(),
    value = fileSize(bytes);
  return (
    <span className="shrink-0 text-sm text-muted-foreground">
      {intl.formatNumber(value.size, {
        maximumFractionDigits: fileSizeFractionalDigits(value.unit),
      })}{" "}
      {formatFileSizeUnit(value.unit)}
    </span>
  );
}
