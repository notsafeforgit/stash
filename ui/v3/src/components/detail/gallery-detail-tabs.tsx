/**
 * Gallery detail tab panel components.
 */

import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import type * as GQL from "src/core/generated-graphql";
import { Badge } from "src/components/ui/badge";
import { MetaRow } from "src/components/detail/meta-row";
import { CustomFieldsRows } from "src/components/detail/custom-fields-rows";
import { FilterUrlLink } from "src/components/shared/filter-url-link";
import NavUtils from "src/utils/navigation";
import {
  CreatedUpdatedMetaRows,
  FileModTimeMetaRows,
} from "src/components/detail/timestamp-meta-rows";

type GalleryData = NonNullable<GQL.FindGalleryQuery["findGallery"]>;

export function GalleryDetailsTab({ gallery }: { gallery: GalleryData }) {
  const intl = useIntl();

  return (
    <dl className="grid m-0 p-0">
      {gallery.details && (
        <MetaRow
          label={intl.formatMessage({
            id: "details",
            defaultMessage: "Details",
          })}
        >
          <p className="m-0 whitespace-pre-wrap">{gallery.details}</p>
        </MetaRow>
      )}

      {gallery.date && (
        <MetaRow
          label={intl.formatMessage({ id: "date", defaultMessage: "Date" })}
        >
          {gallery.date}
        </MetaRow>
      )}

      {gallery.code && (
        <MetaRow
          selectableText
          label={intl.formatMessage({
            id: "scene_code",
            defaultMessage: "Scene code",
          })}
        >
          {gallery.code}
        </MetaRow>
      )}

      {gallery.photographer && (
        <MetaRow
          label={intl.formatMessage({
            id: "photographer",
            defaultMessage: "Photographer",
          })}
        >
          <FilterUrlLink
            href={NavUtils.makePhotographerGalleriesUrl(gallery.photographer)}
          >
            {gallery.photographer}
          </FilterUrlLink>
        </MetaRow>
      )}

      {gallery.rating100 != null && (
        <MetaRow
          label={intl.formatMessage({ id: "rating", defaultMessage: "Rating" })}
        >
          {gallery.rating100}
        </MetaRow>
      )}

      {gallery.urls && gallery.urls.length > 0 && (
        <MetaRow
          selectableText
          label={intl.formatMessage({ id: "urls", defaultMessage: "URLs" })}
        >
          <div className="flex flex-col gap-0.5">
            {gallery.urls.map((u) => (
              <a
                key={u}
                href={u}
                target="_blank"
                rel="noopener noreferrer"
                className="text-primary hover:underline break-all"
              >
                {u}
              </a>
            ))}
          </div>
        </MetaRow>
      )}

      {gallery.studio && (
        <MetaRow
          label={intl.formatMessage({ id: "studio", defaultMessage: "Studio" })}
        >
          <Badge
            variant="secondary"
            render={
              <Link
                to="/studios/$studioId"
                params={{ studioId: gallery.studio.id }}
              />
            }
          >
            {gallery.studio.name}
          </Badge>
        </MetaRow>
      )}

      {gallery.performers && gallery.performers.length > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "performers",
            defaultMessage: "Performers",
          })}
        >
          <div className="flex flex-wrap gap-1">
            {gallery.performers.map((p) => (
              <Badge
                key={p.id}
                variant="secondary"
                render={
                  <Link
                    to="/performers/$performerId"
                    params={{ performerId: p.id }}
                  />
                }
              >
                {p.name}
              </Badge>
            ))}
          </div>
        </MetaRow>
      )}

      {gallery.tags && gallery.tags.length > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "tags", defaultMessage: "Tags" })}
        >
          <div className="flex flex-wrap gap-1">
            {gallery.tags.map((t) => (
              <Badge
                key={t.id}
                variant="secondary"
                render={<Link to="/tags/$tagId" params={{ tagId: t.id }} />}
              >
                {t.name}
              </Badge>
            ))}
          </div>
        </MetaRow>
      )}

      <MetaRow
        label={intl.formatMessage({ id: "images", defaultMessage: "Images" })}
      >
        {gallery.image_count}
      </MetaRow>

      {gallery.scenes && gallery.scenes.length > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "scenes", defaultMessage: "Scenes" })}
        >
          {gallery.scenes.length}
        </MetaRow>
      )}
      <FileModTimeMetaRows files={gallery.files} />
      <CreatedUpdatedMetaRows
        createdAt={gallery.created_at}
        updatedAt={gallery.updated_at}
      />
      <CustomFieldsRows values={gallery.custom_fields} />
    </dl>
  );
}
