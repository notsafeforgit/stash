/**
 * Fingerprint rows for the scene/image file-info tabs. The phash row
 * links to the entity list filtered by that hash (v2.5 parity) so the
 * user can jump straight to all entries sharing a perceptual hash.
 */
import { useIntl } from "react-intl";
import NavUtils from "src/utils/navigation";
import { MetaRow } from "src/components/detail/meta-row";
import { FilterUrlLink } from "src/components/shared/filter-url-link";

interface Fingerprint {
  type: string;
  value: string;
}

export function FingerprintMetaRows({
  fingerprints,
  mode,
}: {
  fingerprints: readonly Fingerprint[];
  mode: "scenes" | "images";
}) {
  const intl = useIntl();

  return (
    <>
      {fingerprints.map((fp) => {
        if (fp.type !== "phash") {
          return (
            <MetaRow key={fp.type} label={fp.type.toUpperCase()}>
              <span className="font-mono text-xs">{fp.value}</span>
            </MetaRow>
          );
        }

        const href =
          mode === "scenes"
            ? NavUtils.makeScenesPHashMatchUrl(fp.value)
            : NavUtils.makeImagesPHashMatchUrl(fp.value);
        return (
          <MetaRow key={fp.type} label={fp.type.toUpperCase()}>
            <FilterUrlLink
              href={href}
              title={intl.formatMessage({
                id: "media_info.phash_meaning",
                defaultMessage: "Perceptual Hash",
              })}
              className="font-mono text-xs"
            >
              {fp.value}
            </FilterUrlLink>
          </MetaRow>
        );
      })}
    </>
  );
}
