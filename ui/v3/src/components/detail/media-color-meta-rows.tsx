import { useIntl } from "react-intl";
import {
  MediaColorBadge,
  mediaColorDetails,
  type MediaColorMetadata,
} from "@/components/shared/media-color-badge";
import { MetaRow } from "./meta-row";

/** Source-file colour information, independent of the playback rendition.
 * Expanded rows keep the technical metadata readable on touch devices too. */
export function MediaColorMetaRows({
  file,
  expanded = false,
}: {
  file: MediaColorMetadata;
  expanded?: boolean;
}) {
  const intl = useIntl();
  return (
    <>
      <MetaRow
        label={intl.formatMessage({
          id: "media_info.dynamic_range",
          defaultMessage: "Dynamic range",
        })}
      >
        <MediaColorBadge file={file} />
      </MetaRow>
      {expanded &&
        mediaColorDetails(file, intl).map(({ id, defaultMessage, value }) => (
          <MetaRow key={id} label={intl.formatMessage({ id, defaultMessage })}>
            {value}
          </MetaRow>
        ))}
    </>
  );
}
