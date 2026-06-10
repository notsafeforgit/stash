/**
 * Studio detail tab panel components.
 */

import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import type * as GQL from "src/core/generated-graphql";
import { Badge } from "src/components/ui/badge";
import { MetaRow } from "src/components/detail/meta-row";
import { CustomFieldsRows } from "src/components/detail/custom-fields-rows";

type StudioData = NonNullable<GQL.FindStudioQuery["findStudio"]>;

export function StudioDetailsTab({ studio }: { studio: StudioData }) {
  const intl = useIntl();

  return (
    <dl className="grid m-0 p-0">
      {studio.details && (
        <MetaRow
          label={intl.formatMessage({
            id: "details",
            defaultMessage: "Details",
          })}
        >
          <p className="m-0 whitespace-pre-wrap">{studio.details}</p>
        </MetaRow>
      )}

      {studio.urls && studio.urls.length > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "urls", defaultMessage: "URLs" })}
        >
          <div className="flex flex-col gap-0.5">
            {studio.urls.map((u) => (
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

      {studio.aliases && studio.aliases.length > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "aliases",
            defaultMessage: "Aliases",
          })}
        >
          <div className="flex flex-col gap-0.5">
            {studio.aliases.map((a) => (
              <span key={a}>{a}</span>
            ))}
          </div>
        </MetaRow>
      )}

      {studio.parent_studio && (
        <MetaRow
          label={intl.formatMessage({
            id: "parent_studio",
            defaultMessage: "Parent studio",
          })}
        >
          <Badge
            variant="secondary"
            render={
              <Link
                to="/studios/$studioId"
                params={{ studioId: studio.parent_studio.id }}
              />
            }
          >
            {studio.parent_studio.name}
          </Badge>
        </MetaRow>
      )}

      {studio.child_studios && studio.child_studios.length > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "child_studios",
            defaultMessage: "Sub-studios",
          })}
        >
          <div className="flex flex-wrap gap-1">
            {studio.child_studios.map((c) => (
              <Badge
                key={c.id}
                variant="secondary"
                render={
                  <Link to="/studios/$studioId" params={{ studioId: c.id }} />
                }
              >
                {c.name}
              </Badge>
            ))}
          </div>
        </MetaRow>
      )}

      {studio.tags && studio.tags.length > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "tags", defaultMessage: "Tags" })}
        >
          <div className="flex flex-wrap gap-1">
            {studio.tags.map((t) => (
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

      {studio.rating100 != null && (
        <MetaRow
          label={intl.formatMessage({ id: "rating", defaultMessage: "Rating" })}
        >
          {studio.rating100}
        </MetaRow>
      )}

      <MetaRow
        label={intl.formatMessage({ id: "scenes", defaultMessage: "Scenes" })}
      >
        {studio.scene_count}
        {studio.scene_count_all > studio.scene_count && (
          <span className="text-muted-foreground ml-1">
            ({studio.scene_count_all} total)
          </span>
        )}
      </MetaRow>

      {studio.image_count > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "images", defaultMessage: "Images" })}
        >
          {studio.image_count}
          {studio.image_count_all > studio.image_count && (
            <span className="text-muted-foreground ml-1">
              ({studio.image_count_all} total)
            </span>
          )}
        </MetaRow>
      )}

      {studio.gallery_count > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "galleries",
            defaultMessage: "Galleries",
          })}
        >
          {studio.gallery_count}
          {studio.gallery_count_all > studio.gallery_count && (
            <span className="text-muted-foreground ml-1">
              ({studio.gallery_count_all} total)
            </span>
          )}
        </MetaRow>
      )}

      {studio.performer_count > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "performers",
            defaultMessage: "Performers",
          })}
        >
          {studio.performer_count}
          {studio.performer_count_all > studio.performer_count && (
            <span className="text-muted-foreground ml-1">
              ({studio.performer_count_all} total)
            </span>
          )}
        </MetaRow>
      )}

      {studio.group_count > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "groups", defaultMessage: "Groups" })}
        >
          {studio.group_count}
          {studio.group_count_all > studio.group_count && (
            <span className="text-muted-foreground ml-1">
              ({studio.group_count_all} total)
            </span>
          )}
        </MetaRow>
      )}
      <CustomFieldsRows values={studio.custom_fields} />
    </dl>
  );
}
