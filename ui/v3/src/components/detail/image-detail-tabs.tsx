/**
 * Image detail tab panel components.
 */

import React from "react";
import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import type * as GQL from "src/core/generated-graphql";
import { galleryLabel } from "src/lib/gallery-utils";
import { Badge } from "src/components/ui/badge";
import { MetaRow } from "src/components/detail/meta-row";
import { CustomFieldsRows } from "src/components/detail/custom-fields-rows";
import { FingerprintMetaRows } from "src/components/detail/fingerprint-meta-rows";
import { FilterUrlLink } from "src/components/shared/filter-url-link";
import NavUtils from "src/utils/navigation";

type ImageData = NonNullable<GQL.FindImageQuery["findImage"]>;

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function ImageDetailsTab({ image }: { image: ImageData }) {
  const intl = useIntl();

  return (
    <dl className="grid m-0 p-0">
      {image.title && (
        <MetaRow
          label={intl.formatMessage({ id: "title", defaultMessage: "Title" })}
        >
          {image.title}
        </MetaRow>
      )}

      {image.date && (
        <MetaRow
          label={intl.formatMessage({ id: "date", defaultMessage: "Date" })}
        >
          {image.date}
        </MetaRow>
      )}

      {image.code && (
        <MetaRow
          label={intl.formatMessage({
            id: "scene_code",
            defaultMessage: "Scene code",
          })}
        >
          {image.code}
        </MetaRow>
      )}

      {image.photographer && (
        <MetaRow
          label={intl.formatMessage({
            id: "photographer",
            defaultMessage: "Photographer",
          })}
        >
          <FilterUrlLink
            href={NavUtils.makePhotographerImagesUrl(image.photographer)}
          >
            {image.photographer}
          </FilterUrlLink>
        </MetaRow>
      )}

      {image.details && (
        <MetaRow
          label={intl.formatMessage({
            id: "details",
            defaultMessage: "Details",
          })}
        >
          <p className="m-0 whitespace-pre-wrap">{image.details}</p>
        </MetaRow>
      )}

      {image.rating100 != null && (
        <MetaRow
          label={intl.formatMessage({ id: "rating", defaultMessage: "Rating" })}
        >
          {image.rating100}
        </MetaRow>
      )}

      {image.urls && image.urls.length > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "urls", defaultMessage: "URLs" })}
        >
          <div className="flex flex-col gap-0.5">
            {image.urls.map((u) => (
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

      {image.studio && (
        <MetaRow
          label={intl.formatMessage({ id: "studio", defaultMessage: "Studio" })}
        >
          <Badge
            variant="secondary"
            render={
              <Link
                to="/studios/$studioId"
                params={{ studioId: image.studio.id }}
              />
            }
          >
            {image.studio.name}
          </Badge>
        </MetaRow>
      )}

      {image.performers && image.performers.length > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "performers",
            defaultMessage: "Performers",
          })}
        >
          <div className="flex flex-wrap gap-1">
            {image.performers.map((p) => (
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

      {image.tags && image.tags.length > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "tags", defaultMessage: "Tags" })}
        >
          <div className="flex flex-wrap gap-1">
            {image.tags.map((t) => (
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

      {image.galleries && image.galleries.length > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "galleries",
            defaultMessage: "Galleries",
          })}
        >
          <div className="flex flex-col gap-0.5 items-start">
            {image.galleries.map((g) => (
              <Badge
                key={g.id}
                variant="secondary"
                render={
                  <Link
                    to="/galleries/$galleryId"
                    params={{ galleryId: g.id }}
                  />
                }
              >
                {galleryLabel(g)}
              </Badge>
            ))}
          </div>
        </MetaRow>
      )}
      <CustomFieldsRows values={image.custom_fields} />
    </dl>
  );
}

export function ImageFileInfoTab({ image }: { image: ImageData }) {
  const intl = useIntl();

  return (
    <dl className="grid m-0 p-0">
      {image.visual_files.map((f) => (
        <React.Fragment key={f.id}>
          <MetaRow
            label={intl.formatMessage({ id: "path", defaultMessage: "Path" })}
          >
            <span className="font-mono text-xs break-all">{f.path}</span>
          </MetaRow>
          {"width" in f && f.width > 0 && (
            <MetaRow
              label={intl.formatMessage({
                id: "dimensions",
                defaultMessage: "Dimensions",
              })}
            >
              {f.width} × {f.height}
            </MetaRow>
          )}
          <MetaRow
            label={intl.formatMessage({
              id: "filesize",
              defaultMessage: "File size",
            })}
          >
            {formatBytes(f.size)}
          </MetaRow>
          <FingerprintMetaRows fingerprints={f.fingerprints} mode="images" />
        </React.Fragment>
      ))}
    </dl>
  );
}
