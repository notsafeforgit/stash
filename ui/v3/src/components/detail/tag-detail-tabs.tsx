/**
 * Tag detail tab panel components.
 */

import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import type * as GQL from "src/core/generated-graphql";
import { Badge } from "src/components/ui/badge";
import { MetaRow } from "src/components/detail/meta-row";
import { CustomFieldsRows } from "src/components/detail/custom-fields-rows";

type TagData = NonNullable<GQL.FindTagQuery["findTag"]>;

export function TagDetailsTab({ tag }: { tag: TagData }) {
  const intl = useIntl();

  return (
    <dl className="grid m-0 p-0">
      {tag.description && (
        <MetaRow
          label={intl.formatMessage({
            id: "description",
            defaultMessage: "Description",
          })}
        >
          <p className="m-0 whitespace-pre-wrap">{tag.description}</p>
        </MetaRow>
      )}

      {tag.aliases.length > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "aliases",
            defaultMessage: "Aliases",
          })}
        >
          <div className="flex flex-col gap-0.5">
            {tag.aliases.map((a) => (
              <span key={a}>{a}</span>
            ))}
          </div>
        </MetaRow>
      )}

      {tag.parents.length > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "parent_tags",
            defaultMessage: "Parent tags",
          })}
        >
          <div className="flex flex-wrap gap-1">
            {tag.parents.map((p) => (
              <Badge
                key={p.id}
                variant="secondary"
                render={<Link to="/tags/$tagId" params={{ tagId: p.id }} />}
              >
                {p.name}
              </Badge>
            ))}
          </div>
        </MetaRow>
      )}

      {tag.children.length > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "sub_tags",
            defaultMessage: "Sub-tags",
          })}
        >
          <div className="flex flex-wrap gap-1">
            {tag.children.map((c) => (
              <Badge
                key={c.id}
                variant="secondary"
                render={<Link to="/tags/$tagId" params={{ tagId: c.id }} />}
              >
                {c.name}
              </Badge>
            ))}
          </div>
        </MetaRow>
      )}

      <MetaRow
        label={intl.formatMessage({ id: "scenes", defaultMessage: "Scenes" })}
      >
        {tag.scene_count}
        {tag.scene_count_all > tag.scene_count && (
          <span className="text-muted-foreground ml-1">
            ({tag.scene_count_all} total)
          </span>
        )}
      </MetaRow>

      {tag.scene_marker_count > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "markers",
            defaultMessage: "Markers",
          })}
        >
          {tag.scene_marker_count}
          {tag.scene_marker_count_all > tag.scene_marker_count && (
            <span className="text-muted-foreground ml-1">
              ({tag.scene_marker_count_all} total)
            </span>
          )}
        </MetaRow>
      )}

      {tag.image_count > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "images", defaultMessage: "Images" })}
        >
          {tag.image_count}
          {tag.image_count_all > tag.image_count && (
            <span className="text-muted-foreground ml-1">
              ({tag.image_count_all} total)
            </span>
          )}
        </MetaRow>
      )}

      {tag.gallery_count > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "galleries",
            defaultMessage: "Galleries",
          })}
        >
          {tag.gallery_count}
          {tag.gallery_count_all > tag.gallery_count && (
            <span className="text-muted-foreground ml-1">
              ({tag.gallery_count_all} total)
            </span>
          )}
        </MetaRow>
      )}

      {tag.performer_count > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "performers",
            defaultMessage: "Performers",
          })}
        >
          {tag.performer_count}
          {tag.performer_count_all > tag.performer_count && (
            <span className="text-muted-foreground ml-1">
              ({tag.performer_count_all} total)
            </span>
          )}
        </MetaRow>
      )}

      {tag.studio_count > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "studios",
            defaultMessage: "Studios",
          })}
        >
          {tag.studio_count}
          {tag.studio_count_all > tag.studio_count && (
            <span className="text-muted-foreground ml-1">
              ({tag.studio_count_all} total)
            </span>
          )}
        </MetaRow>
      )}

      {tag.group_count > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "groups", defaultMessage: "Groups" })}
        >
          {tag.group_count}
          {tag.group_count_all > tag.group_count && (
            <span className="text-muted-foreground ml-1">
              ({tag.group_count_all} total)
            </span>
          )}
        </MetaRow>
      )}
      <CustomFieldsRows values={tag.custom_fields} />
    </dl>
  );
}
