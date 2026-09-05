/**
 * Group detail tab panel components.
 */

import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import type * as GQL from "src/core/generated-graphql";
import { Badge } from "src/components/ui/badge";
import { MetaRow } from "src/components/detail/meta-row";
import { CustomFieldsRows } from "src/components/detail/custom-fields-rows";
import { FilterUrlLink } from "src/components/shared/filter-url-link";
import NavUtils from "src/utils/navigation";
import { CreatedUpdatedMetaRows } from "src/components/detail/timestamp-meta-rows";

type GroupData = NonNullable<GQL.FindGroupQuery["findGroup"]>;

function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  if (h > 0)
    return `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
  return `${m}:${String(s).padStart(2, "0")}`;
}

export function GroupDetailsTab({ group }: { group: GroupData }) {
  const intl = useIntl();

  return (
    <dl className="grid m-0 p-0">
      {group.synopsis && (
        <MetaRow
          label={intl.formatMessage({
            id: "synopsis",
            defaultMessage: "Synopsis",
          })}
        >
          <p className="m-0 whitespace-pre-wrap">{group.synopsis}</p>
        </MetaRow>
      )}

      {group.date && (
        <MetaRow
          label={intl.formatMessage({ id: "date", defaultMessage: "Date" })}
        >
          {group.date}
        </MetaRow>
      )}

      {group.director && (
        <MetaRow
          label={intl.formatMessage({
            id: "director",
            defaultMessage: "Director",
          })}
        >
          <FilterUrlLink href={NavUtils.makeDirectorGroupsUrl(group.director)}>
            {group.director}
          </FilterUrlLink>
        </MetaRow>
      )}

      {group.duration != null && (
        <MetaRow
          label={intl.formatMessage({
            id: "duration",
            defaultMessage: "Duration",
          })}
        >
          {formatDuration(group.duration)}
        </MetaRow>
      )}

      {group.rating100 != null && (
        <MetaRow
          label={intl.formatMessage({ id: "rating", defaultMessage: "Rating" })}
        >
          {group.rating100}
        </MetaRow>
      )}

      {group.aliases && group.aliases.length > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "aliases",
            defaultMessage: "Aliases",
          })}
        >
          <div className="flex flex-col gap-0.5">
            {group.aliases.split(",").map((a) => (
              <span key={a}>{a.trim()}</span>
            ))}
          </div>
        </MetaRow>
      )}

      {group.urls && group.urls.length > 0 && (
        <MetaRow
          selectableText
          label={intl.formatMessage({ id: "urls", defaultMessage: "URLs" })}
        >
          <div className="flex flex-col gap-0.5">
            {group.urls.map((u) => (
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

      {group.studio && (
        <MetaRow
          label={intl.formatMessage({ id: "studio", defaultMessage: "Studio" })}
        >
          <Badge
            variant="secondary"
            render={
              <Link
                to="/studios/$studioId"
                params={{ studioId: group.studio.id }}
              />
            }
          >
            {group.studio.name}
          </Badge>
        </MetaRow>
      )}

      {group.tags && group.tags.length > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "tags", defaultMessage: "Tags" })}
        >
          <div className="flex flex-wrap gap-1">
            {group.tags.map((t) => (
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

      {group.containing_groups && group.containing_groups.length > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "containing_groups",
            defaultMessage: "In groups",
          })}
        >
          <div className="flex flex-wrap gap-1">
            {group.containing_groups.map((cg) => (
              <Badge
                key={cg.group.id}
                variant="secondary"
                render={
                  <Link
                    to="/groups/$groupId"
                    params={{ groupId: cg.group.id }}
                  />
                }
              >
                {cg.group.name}
              </Badge>
            ))}
          </div>
        </MetaRow>
      )}

      <MetaRow
        label={intl.formatMessage({ id: "scenes", defaultMessage: "Scenes" })}
      >
        {group.scene_count}
        {group.scene_count_all > group.scene_count && (
          <span className="text-muted-foreground ml-1">
            ({group.scene_count_all} total)
          </span>
        )}
      </MetaRow>

      {group.performer_count > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "performers",
            defaultMessage: "Performers",
          })}
        >
          {group.performer_count}
          {group.performer_count_all > group.performer_count && (
            <span className="text-muted-foreground ml-1">
              ({group.performer_count_all} total)
            </span>
          )}
        </MetaRow>
      )}

      {group.sub_group_count > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "sub_groups",
            defaultMessage: "Sub-groups",
          })}
        >
          {group.sub_group_count}
          {group.sub_group_count_all > group.sub_group_count && (
            <span className="text-muted-foreground ml-1">
              ({group.sub_group_count_all} total)
            </span>
          )}
        </MetaRow>
      )}
      <CreatedUpdatedMetaRows
        createdAt={group.created_at}
        updatedAt={group.updated_at}
      />
      <CustomFieldsRows values={group.custom_fields} />
    </dl>
  );
}
